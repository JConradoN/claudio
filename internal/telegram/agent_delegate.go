package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/agentmesh"
)

// agentDelegateRequest holds a parsed agent delegation command.
type agentDelegateRequest struct {
	agent agentmesh.AgentName
	task  string
	raw   string
}

// parseAgentDelegate extracts agent name and task text from a message.
// Supported prefixes: /claude, /gemini, /mesh.
func parseAgentDelegate(text string) (agentDelegateRequest, bool) {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)

	for _, prefix := range []string{"/claude ", "/claude\n"} {
		if strings.HasPrefix(lower, prefix) {
			task := strings.TrimSpace(trimmed[len(prefix):])
			if task == "" {
				return agentDelegateRequest{}, false
			}
			return agentDelegateRequest{agent: agentmesh.AgentClaude, task: task, raw: trimmed}, true
		}
	}
	for _, prefix := range []string{"/gemini ", "/gemini\n"} {
		if strings.HasPrefix(lower, prefix) {
			task := strings.TrimSpace(trimmed[len(prefix):])
			if task == "" {
				return agentDelegateRequest{}, false
			}
			return agentDelegateRequest{agent: agentmesh.AgentGemini, task: task, raw: trimmed}, true
		}
	}
	// /mesh (status) — no agent/task, handled separately
	if lower == "/mesh" || strings.HasPrefix(lower, "/mesh ") || strings.HasPrefix(lower, "/mesh\n") {
		return agentDelegateRequest{agent: "", task: "status", raw: trimmed}, true
	}

	return agentDelegateRequest{}, false
}

// cmdAgentDelegate handles /claude, /gemini, and /mesh commands.
// For delegation commands: sends an immediate ack, runs the CLI in a goroutine,
// then sends the result back to the same chat/thread.
func (bc *BotController) cmdAgentDelegate(c telebot.Context, text string) (string, error) {
	req, ok := parseAgentDelegate(text)
	if !ok {
		return "Sintaxe: `/claude <tarefa>` ou `/gemini <tarefa>` ou `/mesh`", nil
	}

	// /mesh = status only, no subprocess
	if req.agent == "" {
		return bc.meshStatus()
	}

	// Capture context values before returning (goroutine will outlive this handler)
	chatID := c.Chat().ID
	threadID := c.Message().ThreadID

	taskID := uuid.New().String()
	taskSummary := summarizeTask(req.task)

	// Open store — use the real agent-mesh DB on this host
	store, err := agentmesh.Open(agentmesh.DefaultPath())
	if err != nil {
		log.Printf("agentmesh: open store: %v", err)
		return fmt.Sprintf("Erro ao acessar agent-mesh DB: %v", err), nil
	}

	if err := store.CreateTask(taskID, string(req.agent), "delegate", req.task); err != nil {
		_ = store.Close()
		log.Printf("agentmesh: create task: %v", err)
		return fmt.Sprintf("Erro ao registrar task: %v", err), nil
	}

	// Launch async — handler returns the ack message immediately
	go bc.runDelegatedTask(store, chatID, threadID, taskID, req)

	return fmt.Sprintf("⏳ Executando **%s**: _%s_\n`task: %s`", req.agent, taskSummary, taskID[:8]), nil
}

// runDelegatedTask runs the CLI subprocess and sends the result to Telegram.
// Owns the store reference and closes it when done.
func (bc *BotController) runDelegatedTask(
	store *agentmesh.Store,
	chatID int64,
	threadID int,
	taskID string,
	req agentDelegateRequest,
) {
	defer func() { _ = store.Close() }()

	// Build prompt with shared context
	prompt := buildDelegatePrompt(store, req)

	_ = store.MarkRunning(taskID)

	ctx, cancel := context.WithTimeout(context.Background(), agentmesh.DefaultTimeout)
	defer cancel()

	result := agentmesh.Run(ctx, req.agent, prompt)

	// Determine final status
	status := "done"
	if !result.Success() {
		status = "failed"
	}

	if err := store.FinishTask(taskID, string(req.agent), status, result.Stdout); err != nil {
		log.Printf("agentmesh: finish task %s: %v", taskID[:8], err)
	}

	// Build Telegram reply
	header := fmt.Sprintf("**%s** concluiu em %.0fs [task `%s`]\n\n",
		req.agent, result.Duration.Seconds(), taskID[:8])
	body := result.TelegramText(req.agent)
	reply := header + body

	if err := SendTextWithThread(bc.bot, &telebot.Chat{ID: chatID}, reply, threadID); err != nil {
		log.Printf("agentmesh: send result to chat %d: %v", chatID, err)
	}
}

// buildDelegatePrompt prepends shared context to the task prompt.
func buildDelegatePrompt(store *agentmesh.Store, req agentDelegateRequest) string {
	ctx, err := store.LoadContext("", 8)
	if err != nil {
		log.Printf("agentmesh: load context: %v", err)
	}

	var sb strings.Builder
	if ctx != "" {
		sb.WriteString("=== CONTEXTO COMPARTILHADO (agent-mesh fox-server) ===\n")
		sb.WriteString(ctx)
		sb.WriteString("=== FIM DO CONTEXTO ===\n\n")
	}
	sb.WriteString("TAREFA:\n")
	sb.WriteString(req.task)
	return sb.String()
}

// meshStatus returns a summary of agent-mesh state for /mesh.
func (bc *BotController) meshStatus() (string, error) {
	store, err := agentmesh.Open(agentmesh.DefaultPath())
	if err != nil {
		return fmt.Sprintf("agent-mesh DB indisponível: %v", err), nil
	}
	defer func() { _ = store.Close() }()

	tasks, err := store.RecentTasksSummary(10)
	if err != nil {
		tasks = "(erro ao consultar tasks)"
	}

	mem, err := store.SharedMemorySummary()
	if err != nil {
		mem = "(erro ao consultar memória)"
	}

	return fmt.Sprintf(
		"**Agent Mesh — Status**\n\n**Tasks recentes:**\n%s\n**Memória compartilhada:**\n%s",
		tasks, mem,
	), nil
}

// summarizeTask returns the first 60 chars of a task for the ack message.
func summarizeTask(task string) string {
	task = strings.TrimSpace(task)
	if len(task) <= 60 {
		return task
	}
	return task[:60] + "…"
}
