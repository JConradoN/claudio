package telegram

import (
	"context"
	"log"
	"strings"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

const (
	classifyTimeout         = 15 * time.Second
	typingIndicatorInterval = 4 * time.Second
	bridgeExecutionTimeout  = 10 * time.Minute
)

func (bc *BotController) processInput(c telebot.Context, text string) error {
	return bc.processInputWithImages(c, text, nil)
}

func (bc *BotController) processInputWithImages(c telebot.Context, text string, images []bridge.ImageAttachment) error {
	// Bootstrap check (needs full context for reply)
	if state, ok := bc.popPendingBootstrap(c.Sender().ID); ok {
		switch state.Step {
		case bootstrapStepAssistant:
			return bc.completeBootstrapAssistant(c, state, text)
		default:
			return bc.completeBootstrapProfile(c, state, text)
		}
	}

	// Command layer — intercept system commands before LLM (needs context for reply)
	if cmd := MatchCommand(text); cmd != nil {
		return bc.handleCommand(c, cmd)
	}

	return bc.runPipeline(c.Chat().ID, c.Message().ThreadID, c.Message().ID, text, images)
}

// runPipeline handles the core message processing after bootstrap/command checks.
// It is shared between the synchronous handler path and async album flush path.
func (bc *BotController) runPipeline(chatID int64, threadID int, messageID int, text string, images []bridge.ImageAttachment) error {
	// 1. Route to agent (sync — fast)
	agent := bc.routeAgent(text)

	// Strip @agent prefix from user text if agent was routed
	userText := text
	if agent != nil {
		if idx := strings.IndexByte(text[1:], ' '); idx != -1 {
			userText = strings.TrimSpace(text[idx+2:])
		} else {
			userText = ""
		}
	}

	if userText == "" {
		userText = text
	}

	// 1b. Auto-detect project path — only on new sessions (no active session)
	// Changing cwd mid-session breaks SDK continue (different project = different session)
	if _, active := bc.sessions.GetWithState(chatID, threadID); !active {
		detectCtx, detectCancel := context.WithTimeout(context.Background(), 3*time.Second)
		detected := bc.detectProjectPath(detectCtx, userText)
		detectCancel()
		if detected != "" {
			bc.sessions.SetCwd(chatID, threadID, detected)
			log.Printf("cwd: auto-detected %s for chat=%d thread=%d", detected, chatID, threadID)
			if bc.resolver != nil {
				if err := runtime.BootstrapProjectMemory(bc.resolver, detected); err != nil {
					log.Printf("cwd: failed to bootstrap project memory for %s: %v", detected, err)
				}
			}
		}
	}

	// 2. Build system prompt (sync — fast)
	systemPrompt, err := bc.buildSystemPrompt(userText, agent, chatID, messageID, threadID)
	if err != nil {
		log.Printf("Failed to build system prompt: %v", err)
		return SendError(bc.bot, &telebot.Chat{ID: chatID}, "Falha ao montar o prompt de sistema.")
	}

	// 3. Build bridge request (sync)
	req := bc.buildBridgeRequest(userText, systemPrompt, agent, chatID, threadID)
	req.Options.Images = images

	// 3b. Vision fallback: if images are present and a vision model is configured,
	// override the model/provider so non-vision models can delegate image analysis.
	if len(images) > 0 {
		if vModel, vProvider := bc.config.VisionFallback(); vModel != "" {
			log.Printf("vision: switching to fallback model %s/%s for image input", vProvider, vModel)
			req.Options.Model = vModel
			if vProvider != "" {
				req.Options.Provider = vProvider
			}
		} else {
			log.Printf("vision: no fallback configured, using default model")
		}
	}

	// 4. Launch async execution — don't block the handler
	go bc.executeAsync(chatID, threadID, messageID, req, userText)

	return nil
}

// routeAgent resolves which agent should handle the message, first by @name
// prefix, then by LLM classification if agents are configured.
func (bc *BotController) routeAgent(text string) *agents.Agent {
	agent := bc.agents.Route(text)
	if agent != nil {
		return agent
	}
	if bc.agents == nil {
		return nil
	}
	classifyCtx, classifyCancel := context.WithTimeout(context.Background(), classifyTimeout)
	defer classifyCancel()
	return bc.agents.Classify(classifyCtx, text, bc.classifyFunc())
}

func (bc *BotController) classifyFunc() agents.ClassifyFunc {
	return func(ctx context.Context, system, prompt string) (string, error) {
		result, err := bc.bridge.ExecuteSync(ctx, bridge.Request{
			Command: "query",
			Prompt:  prompt,
			Options: bridge.RequestOptions{
				Provider:     bc.config.DefaultProvider,
				Model:        bc.config.DefaultModel,
				SystemPrompt: system,
			},
		})
		if err != nil {
			return "", err
		}
		return result.Content, nil
	}
}

// buildBridgeRequest assembles the bridge.Request with agent overrides, session
// resume, and working directory.
func (bc *BotController) buildBridgeRequest(userText, systemPrompt string, agent *agents.Agent, chatID int64, threadID int) bridge.Request {
	req := bridge.Request{
		Command: "query",
		Prompt:  userText,
		Options: bridge.RequestOptions{
			Provider:     bc.config.DefaultProvider,
			Model:        bc.config.DefaultModel,
			SystemPrompt: systemPrompt,
		},
	}

	if agent != nil {
		if agent.Model != "" {
			req.Options.Model = agent.Model
		}
		if agent.Cwd != "" {
			req.Options.Cwd = agent.Cwd
		}
		if len(agent.AllowedTools) > 0 {
			req.Options.AllowedTools = agent.AllowedTools
		}
	}

	// PI resumes sessions by ID/path. Always pass the stored session ID so
	// the Bridge can reuse warm sessions or reopen persisted ones after restart.
	if sessionID, active := bc.sessions.GetWithState(chatID, threadID); sessionID != "" {
		req.Options.Resume = sessionID
		sidPreview := sessionID
		if len(sidPreview) > 8 {
			sidPreview = sidPreview[:8]
		}
		if active {
			req.Options.Continue = true
			log.Printf("bridge: resume sid=%s (continue)", sidPreview)
		} else {
			log.Printf("bridge: resume sid=%s (cold)", sidPreview)
		}
	}

	// Set working directory for the bridge.
	cwd := bc.effectiveCwd(agent, chatID, threadID)
	if cwd != "" {
		req.Options.Cwd = cwd
	} else {
		req.Options.Cwd = bc.botCwd
	}

	return req
}

// executeAsync runs the bridge execution in a goroutine with its own typing
// indicator and progress reporter. Errors are sent directly to the chat since
// the original handler has already returned.
func (bc *BotController) executeAsync(chatID int64, threadID int, messageID int, req bridge.Request, userText string) {
	chat := &telebot.Chat{ID: chatID}

	stopTyping := startChatActionLoop(bc.bot, chat, telebot.Typing, typingIndicatorInterval, threadID)
	defer stopTyping()

	progress := newProgressReporterWithThread(bc.bot, chat, threadID)
	defer progress.Delete()

	ctx, cancel := context.WithTimeout(context.Background(), bridgeExecutionTimeout)
	defer cancel()

	ch, err := bc.bridge.Execute(ctx, req)
	if err != nil {
		log.Printf("Bridge execute error: %v", err)
		if err := SendErrorWithThread(bc.bot, chat, "Falha ao conectar com o processador.", threadID); err != nil {
			log.Printf("Failed to send error to chat %d: %v", chat.ID, err)
		}
		return
	}

	outcome := bc.processBridgeEventsAsyncWithThread(chat, ch, progress, userText, messageID, threadID)

	if outcome == outcomeSuccess {
		bc.bridgeFailures.reset()
		return
	}
	if outcome != outcomeProcessDeath {
		return
	}

	// --- RETRY PATH: bridge died mid-request ---
	bc.bridgeFailures.record()
	log.Printf("bridge: process died mid-request, retrying for chat=%d thread=%d", chatID, threadID)

	if bc.bridgeFailures.inCooldown() {
		log.Printf("bridge: in cooldown, skipping retry for chat=%d", chatID)
		_ = SendErrorWithThread(bc.bot, chat, "Processador temporariamente indisponível. Tente novamente em alguns segundos.", threadID)
		return
	}

	var reconnectMsg *telebot.Message
	reconnectMsg, _ = bc.bot.Send(chat, "⚡ Reconectando...", &telebot.SendOptions{ThreadID: threadID})

	retryReq := req
	retryReq.Options.Continue = false
	retryReq.RequestID = ""
	if sid := bc.sessions.Get(chatID, threadID); sid != "" {
		retryReq.Options.Resume = sid
		log.Printf("bridge: retry with resume sid=%s", sid[:8])
	}

	ch, err = bc.bridge.Execute(ctx, retryReq)
	bc.deleteMessage(reconnectMsg)
	if err != nil {
		log.Printf("bridge: retry failed for chat=%d: %v", chatID, err)
		_ = SendErrorWithThread(bc.bot, chat, "Processador reiniciado mas não conseguiu completar. Tente novamente.", threadID)
		return
	}

	outcome = bc.processBridgeEventsAsyncWithThread(chat, ch, progress, userText, messageID, threadID)

	switch outcome {
	case outcomeSuccess:
		bc.bridgeFailures.reset()
	case outcomeProcessDeath:
		bc.bridgeFailures.record()
		_ = SendError(bc.bot, chat, "Processador reiniciado mas não conseguiu completar. Tente novamente.")
	}
}

// deleteMessage removes a Telegram message if it exists.
func (bc *BotController) deleteMessage(msg *telebot.Message) {
	if msg != nil && bc.bot != nil {
		if err := bc.bot.Delete(msg); err != nil {
			log.Printf("Failed to delete reconnect message: %v", err)
		}
	}
}

// processBridgeEventsAsync reads bridge events and sends responses to the
// Telegram chat. Returns the outcome so the caller can decide whether to retry.
func (bc *BotController) processBridgeEventsAsync(chat *telebot.Chat, ch <-chan bridge.Event, progress *progressReporter, userText string, messageID int) bridgeOutcome {
	return bc.processBridgeEventsAsyncWithThread(chat, ch, progress, userText, messageID, 0)
}

func (bc *BotController) processBridgeEventsAsyncWithThread(chat *telebot.Chat, ch <-chan bridge.Event, progress *progressReporter, userText string, messageID int, threadID int) bridgeOutcome {
	var assistantText strings.Builder

	for ev := range ch {
		switch ev.Type {
		case "system":
			if ev.SessionID != "" {
				sidPreview := ev.SessionID
				if len(sidPreview) > 8 {
					sidPreview = sidPreview[:8]
				}
				log.Printf("session store: chat=%d thread=%d sid=%s", chat.ID, threadID, sidPreview)
				bc.sessions.Set(chat.ID, threadID, ev.SessionID)
			}

		case "tool_use":
			toolName := ev.Name
			if toolName == "" {
				toolName = "tool"
			}
			progress.ReportTool(toolName)

		case "assistant":
			content := ev.Text
			if content == "" {
				content = ev.Content
			}
			assistantText.WriteString(content)

		case "result":
			content := ev.Text
			if content == "" {
				content = ev.Content
			}
			if content != "" {
				// Bridge protocol contract: result.Content is the FINAL answer.
				// Replace any partial assistant text accumulated from earlier
				// assistant events (the SDK swaps in the complete result).
				prior := assistantText.String()
				if prior != "" && prior != content {
					log.Printf("bridge: result.Content diverges from accumulated assistant text (%d vs %d chars)", len(prior), len(content))
				}
				assistantText.Reset()
				assistantText.WriteString(content)
			}

			if ev.CostUSD > 0 || ev.NumTurns > 0 {
				if bc.tracker.RecordUsage(chat.ID, ev.NumTurns, ev.CostUSD, bc.config.MaxSessionTokens, ev.InputTokens, ev.OutputTokens) {
					log.Printf("session auto-reset: chat=%d threshold=%d", chat.ID, bc.config.MaxSessionTokens)
					if bc.dreamer != nil {
						cwd := bc.sessions.GetCwd(chat.ID, threadID)
						bc.dreamer.FlushNudge(chat.ID, threadID, cwd, bc.nudgeBuffer)
						bc.invalidateMemoryDirs(chat.ID, threadID, cwd)
					}
					bc.sessions.Clear(chat.ID, threadID)
					bc.tracker.Clear(chat.ID)
				} else {
					usage := bc.tracker.Get(chat.ID)
					log.Printf("session usage: chat=%d %s", chat.ID, usage)
				}
			}

			finalText := strings.TrimSpace(assistantText.String())
			if finalText == "" {
				finalText = "(sem resposta)"
			}

			if bc.orchestrator != nil {
				if plan, err := bc.orchestrator.ExtractPlan(finalText); err == nil && plan != nil {
					log.Printf("Execution plan detected with %d tasks", len(plan.Tasks))
					displayText := orchestrator.StripPlanBlock(finalText)
					if displayText != "" {
						_ = SendTextReplyWithThread(bc.bot, chat, displayText, threadID)
					}
					go bc.executeApprovedPlan(chat, messageID, plan)
					return outcomeSuccess
				}
			}

			if err := SendTextReplyWithThread(bc.bot, chat, finalText, threadID); err != nil {
				log.Printf("Failed to send reply to chat %d: %v", chat.ID, err)
			}
			if bc.dreamer != nil {
				bc.dreamer.AfterTurn()
				cwd := bc.sessions.GetCwd(chat.ID, threadID)
				bc.nudgeBuffer.AddTurn(chat.ID, userText, finalText)
				bc.dreamer.AfterTurnNudge(chat.ID, threadID, cwd, bc.nudgeBuffer)
				bc.invalidateMemoryDirs(chat.ID, threadID, cwd)
			}
			return outcomeSuccess

		case "error":
			errMsg := ev.Message
			if errMsg == "" {
				errMsg = ev.Content
			}
			if errMsg == "" {
				errMsg = "Erro desconhecido no processador."
			}
			log.Printf("Bridge error: %s", errMsg)
			if err := SendErrorWithThread(bc.bot, chat, errMsg, threadID); err != nil {
				log.Printf("Failed to send error to chat %d: %v", chat.ID, err)
			}
			return outcomeLLMError

		default:
			log.Printf("Bridge event (ignored): %s", ev.Type)
		}
	}

	// Channel closed without terminal event = process died
	return outcomeProcessDeath
}
