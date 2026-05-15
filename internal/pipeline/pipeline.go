package pipeline

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

const (
	classifyTimeout        = 15 * time.Second
	bridgeExecutionTimeout = 10 * time.Minute
)

// Process handles a user message after transport-level bootstrap and command checks.
func (s *Service) Process(chatID int64, threadID int, messageID int, text string, images []bridge.ImageAttachment) error {
	if s == nil {
		return errors.New("pipeline service is nil")
	}
	if s.output == nil {
		return errors.New("pipeline output is nil")
	}

	agent := s.routeAgent(text)
	userText := stripAgentPrefix(text, agent)

	if _, active := s.sessions.GetWithState(chatID, threadID); !active {
		s.autoDetectProject(chatID, threadID, userText)
	}

	systemPrompt, err := s.buildSystemPrompt(userText, agent, chatID, messageID, threadID)
	if err != nil {
		log.Printf("Failed to build system prompt: %v", err)
		return s.output.SendError(chatID, threadID, "Falha ao montar o prompt de sistema.")
	}

	req := s.buildBridgeRequest(userText, systemPrompt, agent, chatID, threadID)
	req.Options.Images = images
	s.applyVisionFallback(&req, images)

	go s.executeAsync(chatID, threadID, messageID, req, userText)
	return nil
}

func stripAgentPrefix(text string, agent *agents.Agent) string {
	if agent == nil {
		return text
	}
	if idx := strings.IndexByte(text[1:], ' '); idx != -1 {
		if stripped := strings.TrimSpace(text[idx+2:]); stripped != "" {
			return stripped
		}
	}
	return text
}

func (s *Service) autoDetectProject(chatID int64, threadID int, userText string) {
	detectCtx, detectCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer detectCancel()

	detected := s.detectProjectPath(detectCtx, userText)
	if detected == "" {
		return
	}

	s.sessions.SetCwd(chatID, threadID, detected)
	log.Printf("cwd: auto-detected %s for chat=%d thread=%d", detected, chatID, threadID)
	if s.resolver == nil {
		return
	}
	if err := runtime.BootstrapProjectMemory(s.resolver, detected); err != nil {
		log.Printf("cwd: failed to bootstrap project memory for %s: %v", detected, err)
	}
}

func (s *Service) applyVisionFallback(req *bridge.Request, images []bridge.ImageAttachment) {
	if len(images) == 0 || s.config == nil {
		return
	}
	if vModel, vProvider := s.config.VisionFallback(); vModel != "" {
		log.Printf("vision: switching to fallback model %s/%s for image input", vProvider, vModel)
		req.Options.Model = vModel
		if vProvider != "" {
			req.Options.Provider = vProvider
		}
		return
	}
	log.Printf("vision: no fallback configured, using default model")
}

// routeAgent resolves which agent should handle the message, first by @name
// prefix, then by LLM classification if agents are configured.
func (s *Service) routeAgent(text string) *agents.Agent {
	if s.agents == nil {
		return nil
	}
	agent := s.agents.Route(text)
	if agent != nil {
		return agent
	}
	classifyCtx, classifyCancel := context.WithTimeout(context.Background(), classifyTimeout)
	defer classifyCancel()
	return s.agents.Classify(classifyCtx, text, s.classifyFunc())
}

func (s *Service) classifyFunc() agents.ClassifyFunc {
	return func(ctx context.Context, system, prompt string) (string, error) {
		result, err := s.bridge.ExecuteSync(ctx, bridge.Request{
			Command: "query",
			Prompt:  prompt,
			Options: bridge.RequestOptions{
				Provider:     s.config.DefaultProvider,
				Model:        s.config.DefaultModel,
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
func (s *Service) buildBridgeRequest(userText, systemPrompt string, agent *agents.Agent, chatID int64, threadID int) bridge.Request {
	req := bridge.Request{
		Command: "query",
		Prompt:  userText,
		Options: bridge.RequestOptions{
			Provider:     s.config.DefaultProvider,
			Model:        s.config.DefaultModel,
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

	if sessionID, active := s.sessions.GetWithState(chatID, threadID); sessionID != "" {
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

	if cwd := s.effectiveCwd(agent, chatID, threadID); cwd != "" {
		req.Options.Cwd = cwd
	} else {
		req.Options.Cwd = s.botCwd
	}

	return req
}

// executeAsync runs bridge execution with typing/progress reporting.
func (s *Service) executeAsync(chatID int64, threadID int, messageID int, req bridge.Request, userText string) {
	stopTyping := s.output.StartTyping(chatID, threadID)
	defer stopTyping()

	progress := s.output.NewProgress(chatID, threadID)
	defer progress.Delete()

	ctx, cancel := context.WithTimeout(context.Background(), bridgeExecutionTimeout)
	defer cancel()

	ch, err := s.bridge.Execute(ctx, req)
	if err != nil {
		log.Printf("Bridge execute error: %v", err)
		if err := s.output.SendError(chatID, threadID, "Falha ao conectar com o processador."); err != nil {
			log.Printf("Failed to send error to chat %d: %v", chatID, err)
		}
		return
	}

	outcome := s.ProcessBridgeEvents(chatID, threadID, messageID, ch, progress, userText)
	if outcome == OutcomeSuccess {
		s.bridgeFailures.reset()
		return
	}
	if outcome != OutcomeProcessDeath {
		return
	}

	s.bridgeFailures.record()
	log.Printf("bridge: process died mid-request, retrying for chat=%d thread=%d", chatID, threadID)

	if s.bridgeFailures.inCooldown() {
		log.Printf("bridge: in cooldown, skipping retry for chat=%d", chatID)
		_ = s.output.SendError(chatID, threadID, "Processador temporariamente indisponível. Tente novamente em alguns segundos.")
		return
	}

	reconnectMsg, _ := s.output.SendText(chatID, threadID, "⚡ Reconectando...")

	retryReq := req
	retryReq.Options.Continue = false
	retryReq.RequestID = ""
	if sid := s.sessions.Get(chatID, threadID); sid != "" {
		retryReq.Options.Resume = sid
		log.Printf("bridge: retry with resume sid=%s", shortSessionID(sid))
	}

	ch, err = s.bridge.Execute(ctx, retryReq)
	s.output.DeleteMessage(reconnectMsg)
	if err != nil {
		log.Printf("bridge: retry failed for chat=%d: %v", chatID, err)
		_ = s.output.SendError(chatID, threadID, "Processador reiniciado mas não conseguiu completar. Tente novamente.")
		return
	}

	outcome = s.ProcessBridgeEvents(chatID, threadID, messageID, ch, progress, userText)
	s.handleRetryOutcome(chatID, threadID, outcome)
}

func shortSessionID(sid string) string {
	if len(sid) > 8 {
		return sid[:8]
	}
	return sid
}

func (s *Service) handleRetryOutcome(chatID int64, threadID int, outcome Outcome) {
	switch outcome {
	case OutcomeSuccess:
		s.bridgeFailures.reset()
	case OutcomeProcessDeath:
		s.bridgeFailures.record()
		_ = s.output.SendError(chatID, threadID, "Processador reiniciado mas não conseguiu completar. Tente novamente.")
	}
}

// ProcessBridgeEvents reads bridge events and sends responses to the output.
func (s *Service) ProcessBridgeEvents(chatID int64, threadID int, messageID int, ch <-chan bridge.Event, progress ProgressReporter, userText string) Outcome {
	var assistantText strings.Builder

	for ev := range ch {
		switch ev.Type {
		case "system":
			s.handleSystemEvent(chatID, threadID, ev)
		case "tool_use":
			toolName := ev.Name
			if toolName == "" {
				toolName = "tool"
			}
			progress.ReportTool(toolName)
		case "assistant":
			assistantText.WriteString(eventContent(ev))
		case "result":
			return s.handleResultEvent(chatID, threadID, messageID, ev, &assistantText, userText)
		case "error":
			return s.handleErrorEvent(chatID, threadID, ev)
		default:
			log.Printf("Bridge event (ignored): %s", ev.Type)
		}
	}

	return OutcomeProcessDeath
}

func (s *Service) handleSystemEvent(chatID int64, threadID int, ev bridge.Event) {
	if ev.SessionID == "" {
		return
	}
	log.Printf("session store: chat=%d thread=%d sid=%s", chatID, threadID, shortSessionID(ev.SessionID))
	s.sessions.Set(chatID, threadID, ev.SessionID)
}

func eventContent(ev bridge.Event) string {
	if ev.Text != "" {
		return ev.Text
	}
	return ev.Content
}

func (s *Service) handleResultEvent(chatID int64, threadID int, messageID int, ev bridge.Event, assistantText *strings.Builder, userText string) Outcome {
	content := eventContent(ev)
	if content != "" {
		prior := assistantText.String()
		if prior != "" && prior != content {
			log.Printf("bridge: result.Content diverges from accumulated assistant text (%d vs %d chars)", len(prior), len(content))
		}
		assistantText.Reset()
		assistantText.WriteString(content)
	}

	s.recordUsage(chatID, threadID, ev)
	finalText := strings.TrimSpace(assistantText.String())
	if finalText == "" {
		finalText = "(sem resposta)"
	}

	if s.tryExecutePlan(chatID, threadID, messageID, finalText) {
		return OutcomeSuccess
	}

	if err := s.output.SendReply(chatID, threadID, finalText); err != nil {
		log.Printf("Failed to send reply to chat %d: %v", chatID, err)
	}
	s.afterSuccessfulTurn(chatID, threadID, userText, finalText)
	return OutcomeSuccess
}

func (s *Service) recordUsage(chatID int64, threadID int, ev bridge.Event) {
	if ev.CostUSD <= 0 && ev.NumTurns <= 0 {
		return
	}
	if s.tracker.RecordUsage(chatID, ev.NumTurns, ev.CostUSD, s.config.MaxSessionTokens, ev.InputTokens, ev.OutputTokens) {
		log.Printf("session auto-reset: chat=%d threshold=%d", chatID, s.config.MaxSessionTokens)
		s.flushDreamer(chatID, threadID)
		s.sessions.Clear(chatID, threadID)
		s.tracker.Clear(chatID)
		return
	}
	usage := s.tracker.Get(chatID)
	log.Printf("session usage: chat=%d %s", chatID, usage)
}

func (s *Service) flushDreamer(chatID int64, threadID int) {
	if s.dreamer == nil {
		return
	}
	cwd := s.sessions.GetCwd(chatID, threadID)
	s.dreamer.FlushNudge(chatID, threadID, cwd, s.nudgeBuffer)
	s.InvalidateMemoryDirs(chatID, threadID, cwd)
}

func (s *Service) tryExecutePlan(chatID int64, threadID int, messageID int, finalText string) bool {
	if s.orchestrator == nil {
		return false
	}
	plan, err := s.orchestrator.ExtractPlan(finalText)
	if err != nil || plan == nil {
		return false
	}
	log.Printf("Execution plan detected with %d tasks", len(plan.Tasks))
	if displayText := orchestrator.StripPlanBlock(finalText); displayText != "" {
		_ = s.output.SendReply(chatID, threadID, displayText)
	}
	go s.output.ExecuteApprovedPlan(chatID, messageID, plan)
	return true
}

func (s *Service) afterSuccessfulTurn(chatID int64, threadID int, userText string, finalText string) {
	if s.dreamer == nil {
		return
	}
	s.dreamer.AfterTurn()
	cwd := s.sessions.GetCwd(chatID, threadID)
	s.nudgeBuffer.AddTurn(chatID, userText, finalText)
	s.dreamer.AfterTurnNudge(chatID, threadID, cwd, s.nudgeBuffer)
	s.InvalidateMemoryDirs(chatID, threadID, cwd)
}

func (s *Service) handleErrorEvent(chatID int64, threadID int, ev bridge.Event) Outcome {
	errMsg := ev.Message
	if errMsg == "" {
		errMsg = ev.Content
	}
	if errMsg == "" {
		errMsg = "Erro desconhecido no processador."
	}
	log.Printf("Bridge error: %s", errMsg)
	if err := s.output.SendError(chatID, threadID, errMsg); err != nil {
		log.Printf("Failed to send error to chat %d: %v", chatID, err)
	}
	return OutcomeLLMError
}
