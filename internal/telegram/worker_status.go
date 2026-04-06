package telegram

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/kocar/aurelia/internal/orchestrator"
	"gopkg.in/telebot.v3"
)

// workerStatusReporter manages per-worker status messages in Telegram.
type workerStatusReporter struct {
	bot      *telebot.Bot
	chat     *telebot.Chat
	messages map[string]*telebot.Message // taskID → status message
	mu       sync.Mutex
}

func newWorkerStatusReporter(bot *telebot.Bot, chat *telebot.Chat) *workerStatusReporter {
	return &workerStatusReporter{
		bot:      bot,
		chat:     chat,
		messages: make(map[string]*telebot.Message),
	}
}

// SendStart sends the initial status message for a worker.
func (r *workerStatusReporter) SendStart(taskID, description string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	text := fmt.Sprintf("⚙️ <b>%s</b> — %s", escapeHTML(taskID), escapeHTML(description))
	msg, err := r.bot.Send(r.chat, text, telebot.ModeHTML)
	if err != nil {
		log.Printf("Worker status send error (%s): %v", taskID, err)
		return
	}
	r.messages[taskID] = msg
}

// UpdateProgress edits the status message with the current tool being used.
func (r *workerStatusReporter) UpdateProgress(taskID, toolName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	msg, ok := r.messages[taskID]
	if !ok || msg == nil {
		return
	}

	label := toolDisplayName(toolName)
	text := fmt.Sprintf("⚙️ <b>%s</b> — %s", escapeHTML(taskID), escapeHTML(label))
	if _, err := r.bot.Edit(msg, text, telebot.ModeHTML); err != nil {
		log.Printf("Worker status edit error (%s): %v", taskID, err)
	}
}

// MarkDone edits the status message to show completion.
func (r *workerStatusReporter) MarkDone(taskID string, durationMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	msg, ok := r.messages[taskID]
	if !ok || msg == nil {
		return
	}

	duration := formatDuration(durationMs)
	text := fmt.Sprintf("✅ <b>%s</b> — Concluído (%s)", escapeHTML(taskID), duration)
	if _, err := r.bot.Edit(msg, text, telebot.ModeHTML); err != nil {
		log.Printf("Worker status done error (%s): %v", taskID, err)
	}
}

// MarkError edits the status message to show failure.
func (r *workerStatusReporter) MarkError(taskID, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	msg, ok := r.messages[taskID]
	if !ok || msg == nil {
		return
	}

	text := fmt.Sprintf("❌ <b>%s</b> — %s", escapeHTML(taskID), escapeHTML(errMsg))
	if _, err := r.bot.Edit(msg, text, telebot.ModeHTML); err != nil {
		log.Printf("Worker status error (%s): %v", taskID, err)
	}
}

// SendPlanSummary sends a formatted plan summary to the chat.
func (r *workerStatusReporter) SendPlanSummary(plan *orchestrator.Plan, replyToID int) {
	waves, err := plan.ExecutionOrder()
	if err != nil {
		log.Printf("Plan summary error: %v", err)
		return
	}

	var sb strings.Builder
	sb.WriteString("📋 <b>Plano de Execução</b>\n\n")

	for i, wave := range waves {
		fmt.Fprintf(&sb, "<b>Fase %d</b>", i+1)
		if len(wave) > 1 {
			sb.WriteString(" (paralelo)")
		}
		sb.WriteString("\n")
		for _, t := range wave {
			agent := t.Agent
			if agent == "" {
				agent = "worker"
			}
			fmt.Fprintf(&sb, "  • <b>%s</b> [%s] — %s\n", t.ID, agent, escapeHTML(t.Description))
		}
		sb.WriteString("\n")
	}

	opts := &telebot.SendOptions{ParseMode: telebot.ModeHTML}
	if replyToID > 0 {
		opts.ReplyTo = &telebot.Message{ID: replyToID, Chat: r.chat}
	}

	if _, err := r.bot.Send(r.chat, sb.String(), opts); err != nil {
		log.Printf("Plan summary send error: %v", err)
	}
}

func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	seconds := ms / 1000
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	remainSec := seconds % 60
	return fmt.Sprintf("%dm%ds", minutes, remainSec)
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
