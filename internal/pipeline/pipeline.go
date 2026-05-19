package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/runlog"
)

// runLogState tracks per-run state for the run journal.
// mu serializes summary mutations independently of the runLogState map lock,
// preventing data races between recordToolUse/recordToolResult and completeRunLog.
type runLogState struct {
	mu           sync.Mutex
	runID        string
	summary      strings.Builder
	summaryCount int
}

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
func (s *Service) Process(chatID int64, threadID int, messageID int, text string, images []bridge.ImageAttachment, userID int64) error {
	if s == nil {
		return errors.New("pipeline service is nil")
	}
	if s.output == nil {
		return errors.New("pipeline output is nil")
	}

	input := pipelineInput{chatID: chatID, threadID: threadID, messageID: messageID, userID: userID, text: text, images: images}
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
	if s.checkProjectPreflight(input, agent, userText) {
		return
	}
	systemPrompt, err := s.buildSystemPrompt(userText, agent, input.chatID, input.messageID, input.threadID, input.userID)
	if err != nil {
		log.Printf("Failed to build system prompt: %s", redactSecrets(err.Error()))
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

	cwd := s.effectiveCwd(agent, chatID, threadID)
	if cwd != "" {
		req.Options.Cwd = cwd
	} else {
		req.Options.Cwd = s.botCwd
		req.Options.DisallowedTools = appendUniqueTools(req.Options.DisallowedTools, chatModeDisallowedTools...)

		// Diagnostic: log why file tools are disabled — helps debug issues where
		// the model cannot access files despite the user asking to read/analyze code.
		sessionCwd := ""
		if s.sessions != nil {
			sessionCwd = s.sessions.GetCwd(chatID, threadID)
		}
		log.Printf("chat mode: file tools disabled for chat=%d thread=%d (bindings=%v session_cwd=%q effective_cwd=%q bot_cwd=%q)",
			chatID, threadID, s.bindings != nil, sessionCwd, cwd, s.botCwd)
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

	// Start runlog entry
	cwd := s.effectiveCwd(nil, chatID, threadID)
	runLogStarted := s.startRunLog(chatID, threadID, req.RequestID, cwd, userText)

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
			if runLogStarted {
				s.patchContinuityFailure(chatID, threadID, "canceled", "cancelado pelo usuário")
				s.completeRunLog(chatID, threadID, runlog.RunCanceled, "", "cancelado pelo usuário")
			}
			return
		} else {
			log.Printf("Bridge execute error: %s", redactSecrets(err.Error()))
			if runLogStarted {
				redacted := redactSecrets(err.Error())
				s.patchContinuityFailure(chatID, threadID, "failed", redacted)
				s.completeRunLog(chatID, threadID, runlog.RunFailed, "", redacted)
			}
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
			if runLogStarted {
				s.patchContinuityFailure(chatID, threadID, "failed", "")
				s.completeRunLog(chatID, threadID, runlog.RunFailed, "", "")
			}
			return
		}
	}

	s.bridgeFailures.record()
	log.Printf("bridge: process died mid-request, retrying for chat=%d thread=%d", chatID, threadID)

	if runLogStarted {
		s.patchContinuityFailure(chatID, threadID, "failed", "process death, retrying")
		s.completeRunLog(chatID, threadID, runlog.RunFailed, "", "process death, retrying")
	}

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
		log.Printf("bridge: retry failed for chat=%d: %s", chatID, redactSecrets(err.Error()))
		s.patchContinuitySessionCold(chatID, threadID, "bridge retry failed: "+redactSecrets(err.Error()))
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
				log.Printf("bridge: cancel request %s failed: %s", requestID, redactSecrets(err.Error()))
			}
		case <-done:
		}
	}()
	return func() { close(done) }
}

func (s *Service) handleContextOutcome(parentCtx context.Context, ctx context.Context, chatID int64, threadID int) bool {
	if parentCtx.Err() != nil {
		log.Printf("pipeline: run canceled chat=%d thread=%d", chatID, threadID)
		s.patchContinuityFailure(chatID, threadID, "canceled", "cancelado pelo usuário")
		s.completeRunLog(chatID, threadID, runlog.RunCanceled, "", "cancelado pelo usuário")
		return true
	}
	if ctx.Err() == context.DeadlineExceeded {
		log.Printf("pipeline: run timeout chat=%d thread=%d", chatID, threadID)
		s.patchContinuityFailure(chatID, threadID, "timed_out", "timeout")
		s.completeRunLog(chatID, threadID, runlog.RunTimedOut, "", "timeout")
		if s.sessions != nil {
			s.sessions.Deactivate(chatID, threadID)
		}
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
		s.patchContinuitySessionCold(chatID, threadID, "bridge retry process death")
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
			if ev.SessionID != "" {
				s.updateRunLogSession(chatID, threadID, ev.SessionID)
			}
		case "tool_use":
			toolName := ev.Name
			if toolName == "" {
				toolName = "tool"
			}
			progress.ReportTool(toolName)
			s.recordToolUse(chatID, threadID, toolName)
		case "tool_result":
			// Append a truncated, redacted summary to the tool tracking state.
			if s.runLog != nil {
				summary := summarizeToolResult(eventContent(ev))
				if summary != "" {
					s.recordToolResult(chatID, threadID, summary)
				}
			} else {
				log.Printf("Bridge event (ignored): tool_result")
			}
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
	s.patchContinuitySessionID(chatID, threadID, ev.SessionID)
}

func eventContent(ev bridge.Event) string {
	return bridge.EventContent(ev)
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

	needsReset := s.recordUsage(chatID, threadID, ev)
	finalText := strings.TrimSpace(assistantText.String())
	if finalText == "" {
		if emptyResultHadWork(ev) {
			log.Printf("bridge: empty result after work chat=%d thread=%d request=%s turns=%d cost=$%.4f in=%d out=%d",
				chatID, threadID, ev.RequestID, ev.NumTurns, ev.CostUSD, ev.InputTokens, ev.OutputTokens)

			// Deactivate session so next turn does not Continue into a suspect session
			if s.sessions != nil {
				s.sessions.Deactivate(chatID, threadID)
			}

			// Capture tool summary before completeRunLog cleans up the state
			toolSummary := s.getRunToolSummary(chatID, threadID)

			s.patchContinuityFailure(chatID, threadID, "failed", "empty result after work")
			s.completeRunLog(chatID, threadID, runlog.RunFailed, "", "empty result after work")

			recoveryMsg := buildEmptyResultRecoveryMessage(toolSummary)
			if err := s.output.SendError(chatID, threadID, recoveryMsg); err != nil {
				log.Printf("Failed to send recovery message to chat %d: %v", chatID, err)
			}
		} else {
			log.Printf("bridge: empty result (no work) chat=%d thread=%d request=%s",
				chatID, threadID, ev.RequestID)
			s.patchContinuityFailure(chatID, threadID, "failed", "empty result")
			s.completeRunLog(chatID, threadID, runlog.RunFailed, "", "empty result")
			if err := s.output.SendError(chatID, threadID, bridgeEmptyResultMessage); err != nil {
				log.Printf("Failed to send empty-result error to chat %d: %v", chatID, err)
			}
		}

		// Even on empty results, flush remaining nudge buffer and reset if threshold was crossed
		if needsReset {
			s.resetSessionAfterSuccessfulTurn(chatID, threadID)
		}
		s.output.ConfirmMessage(chatID, messageID)
		return OutcomeLLMError
	}

	// Capture runID before completeRunLog cleans up runLogStates.
	successRunID := s.getRunID(chatID, threadID)
	s.completeRunLog(chatID, threadID, runlog.RunCompleted, finalText, "")

	if s.tryExecutePlan(chatID, threadID, messageID, finalText) {
		s.output.ConfirmMessage(chatID, messageID)
		s.afterSuccessfulTurn(chatID, threadID, userText, finalText, successRunID)
		if needsReset {
			s.resetSessionAfterSuccessfulTurn(chatID, threadID)
		}
		return OutcomeSuccess
	}

	if err := s.output.SendReply(chatID, threadID, finalText); err != nil {
		log.Printf("Failed to send reply to chat %d: %v", chatID, err)
	}
	s.output.ConfirmMessage(chatID, messageID)
	s.afterSuccessfulTurn(chatID, threadID, userText, finalText, successRunID)
	if needsReset {
		s.resetSessionAfterSuccessfulTurn(chatID, threadID)
	}
	return OutcomeSuccess
}

// recordUsage checks whether token usage exceeds the session threshold.
// Returns true if the session should be auto-reset.
// The actual reset must be performed by the caller via resetSessionAfterSuccessfulTurn
// AFTER the current turn has been saved to the nudge buffer, ensuring no context loss.
func (s *Service) recordUsage(chatID int64, threadID int, ev bridge.Event) bool {
	if s.config == nil || s.tracker == nil {
		return false
	}
	if ev.CostUSD <= 0 && ev.NumTurns <= 0 {
		return false
	}
	needsReset := s.tracker.RecordUsage(chatID, threadID, ev.NumTurns, ev.CostUSD, s.config.MaxSessionTokens, ev.InputTokens, ev.OutputTokens)
	usage := s.tracker.Get(chatID, threadID)
	log.Printf("session usage: chat=%d thread=%d %s (auto-reset=%t)", chatID, threadID, usage, needsReset)
	return needsReset
}

// resetSessionAfterSuccessfulTurn performs the auto-reset actions that were
// previously inside recordUsage. It must be called AFTER afterSuccessfulTurn
// has saved the current turn to the nudge buffer — otherwise the turn is lost.
// Uses ClearSession to preserve cwd and project binding so the user does not
// lose their working directory after an auto-reset.
func (s *Service) resetSessionAfterSuccessfulTurn(chatID int64, threadID int) {
	if s.config != nil {
		log.Printf("session auto-reset: chat=%d thread=%d threshold=%d", chatID, threadID, s.config.MaxSessionTokens)
	} else {
		log.Printf("session auto-reset: chat=%d thread=%d", chatID, threadID)
	}
	// Patch continuity before clearing session — mark cold with reason
	s.patchContinuitySessionCold(chatID, threadID, "auto-reset")
	s.flushDreamer(chatID, threadID)
	s.sessions.ClearSession(chatID, threadID)
	if s.tracker != nil {
		s.tracker.Clear(chatID, threadID)
	}
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

func (s *Service) afterSuccessfulTurn(chatID int64, threadID int, userText string, finalText string, runID string) {
	// Patch continuity with successful turn state
	s.patchContinuityAfterSuccess(chatID, threadID, userText, finalText, runID)

	if s.dreamer == nil {
		return
	}
	s.dreamer.AfterTurn()
	cwd := s.effectiveCwd(nil, chatID, threadID)
	s.nudgeBuffer.AddTurn(chatID, threadID, userText, finalText)
	s.dreamer.AfterTurnNudge(chatID, threadID, cwd, s.nudgeBuffer)
	s.InvalidateMemoryDirs(chatID, threadID, cwd)
}

// --- Continuity lifecycle helpers ---

// getRunID returns the current runID from runLogStates, or empty string.
// Must be called before completeRunLog, which deletes the state.
func (s *Service) getRunID(chatID int64, threadID int) string {
	key := runLogKey(chatID, threadID)
	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	s.runLogMu.Unlock()
	if ok && state != nil {
		return state.runID
	}
	return ""
}

// patchContinuityAfterSuccess writes successful turn state into the continuity store.
// runID must be captured before completeRunLog (which cleans up runLogStates).
func (s *Service) patchContinuityAfterSuccess(chatID int64, threadID int, userText string, assistantText string, runID string) {
	if s.continuity == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cwd := s.effectiveCwd(nil, chatID, threadID)
	now := time.Now()
	runStatus := "completed"
	sessionCold := false

	sessionID := ""
	if s.sessions != nil {
		sessionID = s.sessions.Get(chatID, threadID)
	}

	err := s.continuity.Patch(ctx, continuity.ConversationKey{ChatID: chatID, ThreadID: threadID}, continuity.StatePatch{
		CWD:                  &cwd,
		LastUserIntent:       &userText,
		LastAssistantSummary: &assistantText,
		LastRunID:            &runID,
		LastRunStatus:        &runStatus,
		SessionID:            &sessionID,
		SessionCold:          &sessionCold,
		UpdatedAt:            now,
	})
	if err != nil {
		log.Printf("continuity: failed to patch after success chat=%d thread=%d: %v", chatID, threadID, err)
	}
}

// patchContinuityFailure writes failure/timeout/error state into the continuity store.
// Must be called BEFORE completeRunLog, since that cleans up the run log state.
func (s *Service) patchContinuityFailure(chatID int64, threadID int, status string, errMsg string) {
	if s.continuity == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	now := time.Now()
	sessionCold := true

	// Capture latest checkpoint and tools from the runLogState
	checkpoint := ""
	tools := ""
	runID := ""
	key := runLogKey(chatID, threadID)
	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	if ok && state != nil {
		runID = state.runID
		state.mu.Lock()
		tools = state.summary.String()
		state.mu.Unlock()
	}
	s.runLogMu.Unlock()

	if tools != "" {
		tools = redactSecrets(tools)
	}

	// Build checkpoint from available info
	cp := buildCheckpoint(runlog.RunStatus(status), "", tools, errMsg)
	checkpoint = redactSecrets(cp)

	cwd := s.effectiveCwd(nil, chatID, threadID)

	sid := ""
	if s.sessions != nil {
		sid = s.sessions.Get(chatID, threadID)
	}

	err := s.continuity.Patch(ctx, continuity.ConversationKey{ChatID: chatID, ThreadID: threadID}, continuity.StatePatch{
		CWD:             &cwd,
		LastRunID:       &runID,
		LastRunStatus:   &status,
		LastCheckpoint:  &checkpoint,
		LastTools:       &tools,
		SessionID:       &sid,
		SessionCold:     &sessionCold,
		ResetReason:     &errMsg,
		UpdatedAt:       now,
	})
	if err != nil {
		log.Printf("continuity: failed to patch failure chat=%d thread=%d: %v", chatID, threadID, err)
	}
}

// patchContinuitySessionCold marks the session as cold with a reset reason.
func (s *Service) patchContinuitySessionCold(chatID int64, threadID int, reason string) {
	if s.continuity == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cold := true
	err := s.continuity.Patch(ctx, continuity.ConversationKey{ChatID: chatID, ThreadID: threadID}, continuity.StatePatch{
		SessionCold: &cold,
		ResetReason: &reason,
		UpdatedAt:   time.Now(),
	})
	if err != nil {
		log.Printf("continuity: failed to patch session cold chat=%d thread=%d: %v", chatID, threadID, err)
	}
}

// patchContinuitySessionID updates the session ID in continuity state.
func (s *Service) patchContinuitySessionID(chatID int64, threadID int, sessionID string) {
	if s.continuity == nil || sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := s.continuity.Patch(ctx, continuity.ConversationKey{ChatID: chatID, ThreadID: threadID}, continuity.StatePatch{
		SessionID: &sessionID,
		UpdatedAt: time.Now(),
	})
	if err != nil {
		log.Printf("continuity: failed to patch session ID chat=%d thread=%d: %v", chatID, threadID, err)
	}
}

func (s *Service) handleErrorEvent(chatID int64, threadID int, messageID int, ev bridge.Event) Outcome {
	errMsg := ev.Message
	if errMsg == "" {
		errMsg = ev.Content
	}
	if errMsg == "" {
		errMsg = "Erro desconhecido no processador."
	}
	redacted := redactSecrets(errMsg)
	log.Printf("Bridge error: %s", redacted)
	s.patchContinuityFailure(chatID, threadID, "failed", redacted)
	s.completeRunLog(chatID, threadID, runlog.RunFailed, "", redacted)
	if err := s.output.SendError(chatID, threadID, redacted); err != nil {
		log.Printf("Failed to send error to chat %d: %v", chatID, err)
	}
	s.output.ConfirmMessage(chatID, messageID)
	return OutcomeLLMError
}

// --- Run log lifecycle ---

func runLogKey(chatID int64, threadID int) string {
	return fmt.Sprintf("%d:%d", chatID, threadID)
}

// startRunLog creates a new runlog entry and stores the per-run state.
// RunID is set to a uuid for durable unique identification across restarts.
// Returns true if the runlog was started.
func (s *Service) startRunLog(chatID int64, threadID int, requestID string, cwd string, prompt string) bool {
	if s.runLog == nil || requestID == "" {
		return false
	}
	key := runLogKey(chatID, threadID)

	s.runLogMu.Lock()
	defer s.runLogMu.Unlock()

	runID := uuid.NewString()
	now := time.Now()
	err := s.runLog.Start(context.Background(), runlog.RunRecord{
		RunID:     runID,
		ChatID:    chatID,
		ThreadID:  threadID,
		RequestID: requestID,
		CWD:       cwd,
		Prompt:    redactSecrets(truncatePrompt(prompt)),
		StartedAt: now,
	})
	if err != nil {
		log.Printf("runlog: failed to start %s: %v", requestID, err)
		return false
	}
	s.runLogStates[key] = &runLogState{runID: runID}
	return true
}

// updateRunLogSession updates the session ID for an active runlog entry.
func (s *Service) updateRunLogSession(chatID int64, threadID int, sessionID string) {
	if s.runLog == nil || sessionID == "" {
		return
	}
	key := runLogKey(chatID, threadID)

	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	s.runLogMu.Unlock()
	if !ok || state == nil {
		return
	}

	if err := s.runLog.Update(context.Background(), runlog.RunUpdate{
		RunID:     state.runID,
		SessionID: &sessionID,
	}); err != nil {
		log.Printf("runlog: failed to update session for %s: %v", state.runID, err)
	}
}

// recordToolUse appends a tool name to the in-memory tool summary for a run.
func (s *Service) recordToolUse(chatID int64, threadID int, toolName string) {
	if s.runLog == nil || toolName == "" {
		return
	}
	key := runLogKey(chatID, threadID)

	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	s.runLogMu.Unlock()
	if !ok || state == nil {
		return
	}

	state.mu.Lock()
	needsUpdate := false
	var toolSummary string
	if state.summary.Len() > 0 {
		state.summary.WriteString(", ")
	}
	state.summary.WriteString(toolName)

	// Persist summary every 5 tools to avoid loss on crash
	state.summaryCount++
	if state.summaryCount%5 == 0 {
		toolSummary = state.summary.String()
		needsUpdate = true
	}
	state.mu.Unlock()

	if needsUpdate {
		if err := s.runLog.Update(context.Background(), runlog.RunUpdate{
			RunID:       state.runID,
			ToolSummary: &toolSummary,
		}); err != nil {
			log.Printf("runlog: failed to persist tool summary for %s: %v", state.runID, err)
		}
	}
}

// recordToolResult appends a summarized tool result to the tool summary.
func (s *Service) recordToolResult(chatID int64, threadID int, summary string) {
	if s.runLog == nil || summary == "" {
		return
	}
	key := runLogKey(chatID, threadID)

	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	s.runLogMu.Unlock()
	if !ok || state == nil {
		return
	}

	state.mu.Lock()
	state.summary.WriteString(" → [")
	state.summary.WriteString(summary)
	state.summary.WriteString("]")
	state.mu.Unlock()
}

// completeRunLog marks the runlog entry with a terminal status and checkpoint.
// All persisted data is redacted before storage to prevent credential leakage.
func (s *Service) completeRunLog(chatID int64, threadID int, status runlog.RunStatus, checkpoint, errMsg string) {
	key := runLogKey(chatID, threadID)

	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	delete(s.runLogStates, key)
	s.runLogMu.Unlock()

	if !ok || state == nil || s.runLog == nil {
		return
	}

	// Capture final tool summary under the per-state lock to serialize
	// with concurrent recordToolUse / recordToolResult mutations.
	state.mu.Lock()
	summary := state.summary.String()
	state.mu.Unlock()

	// Defensive redaction: assistant output may contain credentials.
	summary = redactSecrets(summary)
	checkpoint = redactSecrets(checkpoint)
	errMsg = redactSecrets(errMsg)

	// Build checkpoint
	if checkpoint == "" {
		checkpoint = buildCheckpoint(status, "", summary, errMsg)
	} else {
		checkpoint = buildCheckpoint(status, checkpoint, summary, errMsg)
	}

	if err := s.runLog.Complete(context.Background(), state.runID, status, checkpoint, errMsg); err != nil {
		log.Printf("runlog: failed to complete %s (status=%s): %v", state.runID, status, err)
	}

	// Flush session update with final summary
	if summary != "" {
		if err := s.runLog.Update(context.Background(), runlog.RunUpdate{
			RunID:       state.runID,
			ToolSummary: &summary,
		}); err != nil {
			log.Printf("runlog: failed to update summary for %s: %v", state.runID, err)
		}
	}
}

// buildCheckpoint formats a textual checkpoint from run status and context.
func buildCheckpoint(status runlog.RunStatus, checkpoint, toolSummary, errMsg string) string {
	var sb strings.Builder
	sb.WriteString("Status: ")
	sb.WriteString(string(status))
	if toolSummary != "" {
		sb.WriteString("\nFerramentas: ")
		sb.WriteString(toolSummary)
	}
	if checkpoint != "" {
		sb.WriteString("\nResposta/último resumo: ")
		sb.WriteString(truncateCheckpoint(checkpoint))
	}
	if errMsg != "" {
		sb.WriteString("\nErro: ")
		sb.WriteString(errMsg)
	}
	if status == runlog.RunTimedOut {
		sb.WriteString("\nPróximo passo: continue a partir deste checkpoint")
	}
	return sb.String()
}

func truncatePrompt(prompt string) string {
	const maxPromptBytes = 500
	if len(prompt) > maxPromptBytes {
		// Use rune-aware truncation to avoid splitting multi-byte characters.
		trimmed := prompt
		for len(trimmed) > maxPromptBytes {
			trimmed = trimmed[:len(trimmed)-1]
		}
		// Ensure valid UTF-8 at the boundary.
		for i := 0; i < 4 && len(trimmed) > 0; i++ {
			if trimmed[len(trimmed)-1]&0xC0 != 0x80 {
				break
			}
			trimmed = trimmed[:len(trimmed)-1]
		}
		return trimmed + "..."
	}
	return prompt
}

func truncateCheckpoint(s string) string {
	if len(s) > 2000 {
		return s[:2000] + "..."
	}
	return s
}

// summarizeToolResult produces a truncated, redacted summary of a tool result.
func summarizeToolResult(content string) string {
	if content == "" {
		return ""
	}
	// Redact common secret patterns (API keys, tokens, etc.)
	redacted := redactSecrets(content)
	// Take first 1KB
	truncated := strings.TrimSpace(redacted)
	if len(truncated) > 1024 {
		truncated = truncated[:1024]
	}
	return truncated
}

// Pre-compiled redaction regexes to avoid re-parsing on every call.
var (
	prefixREs    []*regexp.Regexp
	prefixLabels []string
	privateKeyRE *regexp.Regexp
	authRE       *regexp.Regexp
	lineRE       *regexp.Regexp
	jsonSecretRE *regexp.Regexp
)

func init() {
	type pattern struct {
		repl  string
		label string
	}
	prefixPatterns := []pattern{
		// API keys
		{`\bsk-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bpk-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bsk-ant-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bsk-proj-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bsk_live_[A-Za-z0-9]+`, "[STRIPE_KEY_REDACTED]"},
		{`\bsk_test_[A-Za-z0-9]+`, "[STRIPE_KEY_REDACTED]"},
		// Cloud provider keys
		{`\bAKIA[A-Z0-9]{16}`, "[AWS_KEY_REDACTED]"},
		{`\bAIza[0-9A-Za-z_-]{35}`, "[GCP_KEY_REDACTED]"},
		// GitHub tokens
		{`\bghp_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bgho_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bghu_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bghs_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bghr_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bgithub_pat_[0-9A-Za-z_-]+`, "[GH_PAT_REDACTED]"},
		// Other tokens
		{`\bglpat-[A-Za-z0-9_-]{20,}`, "[GL_TOKEN_REDACTED]"},
		{`\bhf_[A-Za-z0-9]{20,}`, "[HF_TOKEN_REDACTED]"},
		{`\bnpm_[A-Za-z0-9]{36}`, "[NPM_TOKEN_REDACTED]"},
		{`\bxox[bpasa]-[A-Za-z0-9-]{20,}`, "[SLACK_TOKEN_REDACTED]"},
		{`\bxapp-[A-Za-z0-9-]{20,}`, "[SLACK_TOKEN_REDACTED]"},
		// Base64-encoded JSON (JWT-like)
		{`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+`, "[JWT_REDACTED]"},
		// AI provider keys
		{`\bxai-[A-Za-z0-9]{20,}`, "[XAI_KEY_REDACTED]"},
	}

	prefixREs = make([]*regexp.Regexp, len(prefixPatterns))
	prefixLabels = make([]string, len(prefixPatterns))
	for i, p := range prefixPatterns {
		prefixREs[i] = regexp.MustCompile(p.repl)
		prefixLabels[i] = p.label
	}

	privateKeyRE = regexp.MustCompile(`(?s)-----BEGIN (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----.*?-----END (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----`)
	authRE = regexp.MustCompile(`(?i)(Authorization:\s*(?:Bearer|Basic)\s+)\S+`)
	lineRE       = regexp.MustCompile(`(password|secret|api_key|api-key|api\.key|apikey|clientsecret|client_secret|access_token|refresh_token|token)\s*[=:]\s*\S+`)
	jsonSecretRE = regexp.MustCompile(`"(?:apiKey|api_key|api-key|api\.key|clientSecret|client_secret|client-secret|client\.secret|accessToken|access_token|access-token|access\.token|refreshToken|refresh_token|refresh-token|refresh\.token|token)"\s*:\s*"[^"]{4,}"`)
}

// RedactSecrets is the public wrapper for redactSecrets, shared with other
// packages (e.g. dream nudge prompt) that need credential redaction.
func RedactSecrets(s string) string { return redactSecrets(s) }

// redactSecrets replaces common credential patterns with [REDACTED].
// All regexes are pre-compiled at init for performance.
func redactSecrets(s string) string {
	result := s
	for i, re := range prefixREs {
		result = re.ReplaceAllString(result, prefixLabels[i])
	}

	// Multi-line block redaction (must run before line splitting)
	result = privateKeyRE.ReplaceAllString(result, "[PRIVATE_KEY_BLOCK_REDACTED]")

	// Header-based auth: Authorization: Bearer xxx, Authorization: Basic xxx
	result = authRE.ReplaceAllString(result, "$1[REDACTED]")

	// Line-based redaction for structured data with known keys
	lines := strings.Split(result, "\n")
	var filtered []string
	for _, line := range lines {
		lower := strings.ToLower(line)
		// Match key=value, key:value, key = value, and key: value patterns
		// Patterns cover: password, secret, api_key, api-key, api.key, apikey,
		// clientsecret, access_token, refresh_token, and generic token.
		if lineRE.MatchString(lower) {
			filtered = append(filtered, "[CREDENTIAL_REDACTED]")
			continue
		}
		// JSON-style embedded secrets: "apiKey":"xxx", "clientSecret":"xxx"
		if jsonSecretRE.MatchString(line) {
			filtered = append(filtered, "[CREDENTIAL_REDACTED]")
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}
