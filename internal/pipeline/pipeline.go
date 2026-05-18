package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
)

const (
	classifyTimeout        = 5 * time.Second
	classifyMinTextLen     = 10
	bridgeExecutionTimeout = 10 * time.Minute

	bridgeConnectErrorMessage = "Falha ao conectar com o processador.\n\n" +
		"Dica: verifique se o daemon está rodando. Se persistir, tente /new para reiniciar a sessão."
	bridgeRetryFailedMessage = "Processador reiniciado mas não conseguiu completar. Tente novamente.\n\n" +
		"Dica: se persistir, use /new para reiniciar a sessão."
	bridgeTimeoutMessage = "Tempo limite atingido antes de concluir.\n\n" +
		"A solicitação foi muito complexa. Tente dividir em partes menores."
)

func bridgeCooldownMessage(remaining time.Duration) string {
	seconds := int((remaining + time.Second - time.Nanosecond) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("⏳ Processador em recuperação. Tente novamente em ~%d segundos.", seconds)
}

func queueStatusMessage(active *activeRun, queueSize int) string {
	description := strings.TrimSpace(active.description())
	queueText := queueStatusSuffix(queueSize)
	if description == "" {
		return "⏳ Ainda estou processando o pedido anterior." + queueText
	}
	return "⏳ Ainda estou processando: " + description + "." + queueText
}

func queueStatusSuffix(queueSize int) string {
	if queueSize <= 0 {
		return ""
	}
	if queueSize == 1 {
		return "\n📥 Fila: 1 mensagem aguardando."
	}
	return fmt.Sprintf("\n📥 Fila: %d mensagens aguardando.", queueSize)
}

func queueAdmittedMessage(active *activeRun) string {
	description := strings.TrimSpace(active.description())
	if description == "" {
		return "📥 Sua mensagem é a próxima na fila."
	}
	return "📥 Ainda estou processando: " + description + ". Sua mensagem será a próxima na fila."
}

// Process handles a user message after transport-level bootstrap and command checks.
func (s *Service) Process(chatID int64, threadID int, messageID int, text string, images []bridge.ImageAttachment) error {
	if s == nil {
		return errors.New("pipeline service is nil")
	}
	if s.output == nil {
		return errors.New("pipeline output is nil")
	}

	input := pipelineInput{chatID: chatID, threadID: threadID, messageID: messageID, text: text, images: images}
	run, admission, active := s.runs.admit(input)
	switch admission {
	case admitStart:
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("pipeline: panic in processRun: %v", r)
				}
			}()
			s.processRun(input, run)
		}()
	case admitCancelOnly:
		_, _ = s.output.SendText(chatID, threadID, "🛑 Interrompendo o pedido anterior.")
		s.output.ConfirmMessage(chatID, messageID)
	case admitSupersede:
		_, _ = s.output.SendText(chatID, threadID, "🔁 Interrompi o pedido anterior e vou seguir com sua correção.")
		s.output.ConfirmMessage(chatID, messageID)
	case admitStatus:
		queueSize := s.runs.queueSize(runKey{chatID: chatID, threadID: threadID})
		_, _ = s.output.SendText(chatID, threadID, queueStatusMessage(active, queueSize))
		s.output.ConfirmMessage(chatID, messageID)
	case admitQueued:
		_, _ = s.output.SendText(chatID, threadID, queueAdmittedMessage(active))
	case admitReplacedQueued:
		_, _ = s.output.SendText(chatID, threadID, "🔁 Atualizei a próxima instrução na fila.")
		s.output.ConfirmMessage(chatID, messageID)
	}
	return nil
}

func (s *Service) processRun(input pipelineInput, run *activeRun) {
	defer s.startQueuedAfter(run)

	agent := s.routeAgent(input.text)
	userText := stripAgentPrefix(input.text, agent)

	if run.ctx.Err() != nil {
		return
	}
	if _, active := s.sessions.GetWithState(input.chatID, input.threadID); !active {
		s.autoDetectProject(input.chatID, input.threadID, userText)
	}

	if run.ctx.Err() != nil {
		return
	}
	systemPrompt, err := s.buildSystemPrompt(userText, agent, input.chatID, input.messageID, input.threadID)
	if err != nil {
		log.Printf("Failed to build system prompt: %v", err)
		_ = s.output.SendError(input.chatID, input.threadID, "Falha ao montar o prompt de sistema.")
		s.output.ConfirmMessage(input.chatID, input.messageID)
		return
	}

	req := s.buildBridgeRequest(userText, systemPrompt, agent, input.chatID, input.threadID)
	req.RequestID = fmt.Sprintf("run-%d", run.id)
	req.Options.Images = input.images
	s.applyVisionFallback(&req, input.images)

	s.executeAsync(run.ctx, input.chatID, input.threadID, input.messageID, req, userText)
}

func (s *Service) startQueuedAfter(run *activeRun) {
	nextRun, nextInput := s.runs.finish(run)
	if nextRun == nil || nextInput == nil {
		return
	}
	go s.processRun(*nextInput, nextRun)
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

	log.Printf("cwd: auto-detected %s for chat=%d thread=%d; not persisted, use /cwd %s to bind", detected, chatID, threadID, detected)
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
// prefix, then by LLM classification if agents are configured. Classification
// is skipped when there are fewer than 2 agents (no choice to make) or when
// the message is too short to carry useful intent — that saves a 5s round-trip
// to the bridge on trivial follow-ups like "ok" or "obrigado".
func (s *Service) routeAgent(text string) *agents.Agent {
	if s.agents == nil {
		return nil
	}
	agent := s.agents.Route(text)
	if agent != nil {
		return agent
	}
	if len(s.agents.Agents()) < 2 {
		return nil
	}
	if len(strings.TrimSpace(text)) < classifyMinTextLen {
		return nil
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
		if len(agent.DisallowedTools) > 0 {
			req.Options.DisallowedTools = agent.DisallowedTools
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
		req.Options.DisallowedTools = appendUniqueTools(req.Options.DisallowedTools, chatModeDisallowedTools...)
	}

	return req
}

var chatModeDisallowedTools = []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep", "LS", "List"}

func appendUniqueTools(existing []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, tool := range existing {
		seen[tool] = struct{}{}
	}
	for _, tool := range additions {
		if _, ok := seen[tool]; ok {
			continue
		}
		existing = append(existing, tool)
		seen[tool] = struct{}{}
	}
	return existing
}

// executeAsync runs bridge execution with typing/progress reporting.
func (s *Service) executeAsync(parentCtx context.Context, chatID int64, threadID int, messageID int, req bridge.Request, userText string) {
	stopTyping := s.output.StartTyping(chatID, threadID)
	defer stopTyping()

	progress := s.output.NewProgress(chatID, threadID)
	defer progress.Delete()

	ctx, cancel := context.WithTimeout(parentCtx, bridgeExecutionTimeout)
	defer cancel()
	cancelDone := s.cancelBridgeOnContextDone(ctx, req.RequestID)
	defer cancelDone()

	var ch <-chan bridge.Event
	var err error

	if s.resilient != nil {
		res := s.resilient.Execute(ctx, req, func(msg string) {
			_, _ = s.output.SendText(chatID, threadID, msg)
		})
		if res.Err != nil {
			err = res.Err
		} else {
			ch = res.Events
		}
	} else {
		ch, err = s.bridge.Execute(ctx, req)
	}

	var outcome Outcome
	if err != nil {
		if errors.Is(err, errProcessDeath) {
			// Let the existing process-death recovery below handle this.
			outcome = OutcomeProcessDeath
		} else if errors.Is(err, context.Canceled) {
			log.Printf("pipeline: run canceled by user chat=%d thread=%d", chatID, threadID)
			return
		} else {
			log.Printf("Bridge execute error: %v", err)
			// Only send generic error when resilient bridge is not in use
			// (it already sends specific messages via onNotify).
			if s.resilient == nil {
				if err := s.output.SendError(chatID, threadID, bridgeConnectErrorMessage); err != nil {
					log.Printf("Failed to send error to chat %d: %v", chatID, err)
				}
			}
			s.output.ConfirmMessage(chatID, messageID)
			return
		}
	} else {
		outcome = s.ProcessBridgeEvents(chatID, threadID, messageID, ch, progress, userText)
		if handled := s.handleContextOutcome(parentCtx, ctx, chatID, threadID); handled {
			s.output.ConfirmMessage(chatID, messageID)
			return
		}
		if outcome == OutcomeSuccess {
			s.bridgeFailures.reset()
			return
		}
		if outcome != OutcomeProcessDeath {
			return
		}
	}

	s.bridgeFailures.record()
	log.Printf("bridge: process died mid-request, retrying for chat=%d thread=%d", chatID, threadID)

	if s.bridgeFailures.inCooldown() {
		remaining := s.bridgeFailures.cooldownRemaining()
		log.Printf("bridge: in cooldown, skipping retry for chat=%d", chatID)
		_ = s.output.SendError(chatID, threadID, bridgeCooldownMessage(remaining))
		s.output.ConfirmMessage(chatID, messageID)
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
		_ = s.output.SendError(chatID, threadID, bridgeRetryFailedMessage)
		s.output.ConfirmMessage(chatID, messageID)
		return
	}

	outcome = s.ProcessBridgeEvents(chatID, threadID, messageID, ch, progress, userText)
	if handled := s.handleContextOutcome(parentCtx, ctx, chatID, threadID); handled {
		s.output.ConfirmMessage(chatID, messageID)
		return
	}
	s.handleRetryOutcome(chatID, threadID, messageID, outcome)
}

func (s *Service) cancelBridgeOnContextDone(ctx context.Context, requestID string) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cancelCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := s.bridge.CancelRequest(cancelCtx, requestID); err != nil {
				log.Printf("bridge: cancel request %s failed: %v", requestID, err)
			}
		case <-done:
		}
	}()
	return func() { close(done) }
}

func (s *Service) handleContextOutcome(parentCtx context.Context, ctx context.Context, chatID int64, threadID int) bool {
	if parentCtx.Err() != nil {
		log.Printf("pipeline: run canceled chat=%d thread=%d", chatID, threadID)
		return true
	}
	if ctx.Err() == context.DeadlineExceeded {
		log.Printf("pipeline: run timeout chat=%d thread=%d", chatID, threadID)
		_ = s.output.SendError(chatID, threadID, bridgeTimeoutMessage)
		return true
	}
	return false
}

func shortSessionID(sid string) string {
	if len(sid) > 8 {
		return sid[:8]
	}
	return sid
}

func (s *Service) handleRetryOutcome(chatID int64, threadID int, messageID int, outcome Outcome) {
	switch outcome {
	case OutcomeSuccess:
		s.bridgeFailures.reset()
	case OutcomeProcessDeath:
		s.bridgeFailures.record()
		_ = s.output.SendError(chatID, threadID, bridgeRetryFailedMessage)
		s.output.ConfirmMessage(chatID, messageID)
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
			return s.handleErrorEvent(chatID, threadID, messageID, ev)
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
		s.output.ConfirmMessage(chatID, messageID)
		s.afterSuccessfulTurn(chatID, threadID, userText, finalText)
		return OutcomeSuccess
	}

	if err := s.output.SendReply(chatID, threadID, finalText); err != nil {
		log.Printf("Failed to send reply to chat %d: %v", chatID, err)
	}
	s.output.ConfirmMessage(chatID, messageID)
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
	cwd := s.effectiveCwd(nil, chatID, threadID)
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
	cwd := s.effectiveCwd(nil, chatID, threadID)
	s.nudgeBuffer.AddTurn(chatID, threadID, userText, finalText)
	s.dreamer.AfterTurnNudge(chatID, threadID, cwd, s.nudgeBuffer)
	s.InvalidateMemoryDirs(chatID, threadID, cwd)
}

func (s *Service) handleErrorEvent(chatID int64, threadID int, messageID int, ev bridge.Event) Outcome {
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
	s.output.ConfirmMessage(chatID, messageID)
	return OutcomeLLMError
}
