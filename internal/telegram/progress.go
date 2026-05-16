package telegram

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"gopkg.in/telebot.v3"
)

// progressEditInterval throttles edits to avoid Telegram rate limits.
// Telegram allows ~1 edit/sec per message; we leave a margin.
const progressEditInterval = 1500 * time.Millisecond

type progressReporter struct {
	bot       *telebot.Bot
	chat      *telebot.Chat
	msg       *telebot.Message
	tools     []string
	threadID  int
	startTime time.Time
	lastText  string
	lastEdit  time.Time
	mu        sync.Mutex
}

func newProgressReporter(bot *telebot.Bot, chat *telebot.Chat) *progressReporter {
	return &progressReporter{bot: bot, chat: chat, startTime: time.Now()}
}

func newProgressReporterWithThread(bot *telebot.Bot, chat *telebot.Chat, threadID int) *progressReporter {
	return &progressReporter{bot: bot, chat: chat, threadID: threadID, startTime: time.Now()}
}

func (p *progressReporter) ReportTool(toolName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	label := toolDisplayName(toolName)
	p.tools = append(p.tools, label)

	text := progressText(p.tools, time.Since(p.startTime))
	if text == p.lastText {
		return
	}

	if p.msg == nil {
		sent, err := p.bot.Send(p.chat, text, &telebot.SendOptions{ThreadID: p.threadID})
		if err != nil {
			log.Printf("Progress send error: %v", err)
			return
		}
		p.msg = sent
		p.lastText = text
		p.lastEdit = time.Now()
		return
	}

	// Throttle edits: Telegram rate-limits per-message edits and progress is
	// purely visual feedback — the user only sees the final assistant reply.
	if time.Since(p.lastEdit) < progressEditInterval {
		return
	}
	_, err := p.bot.Edit(p.msg, text)
	if err != nil {
		log.Printf("Progress edit error: %v", err)
		return
	}
	p.lastText = text
	p.lastEdit = time.Now()
}

func progressText(tools []string, elapsed time.Duration) string {
	display := tools
	if len(display) > 8 {
		display = display[len(display)-8:]
	}
	return fmt.Sprintf("⏱️ %s\n%s", formatProgressDuration(elapsed), strings.Join(display, "\n"))
}

func formatProgressDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int(d.Round(time.Second).Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
}

func (p *progressReporter) Delete() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.msg != nil {
		_ = p.bot.Delete(p.msg)
		p.msg = nil
	}
}

func toolDisplayName(name string) string {
	switch name {
	case "Read":
		return "📖 Reading file..."
	case "Write":
		return "✍️ Writing file..."
	case "Edit":
		return "✏️ Editing file..."
	case "Bash":
		return "⚡ Running command..."
	case "Glob":
		return "🔍 Searching files..."
	case "Grep":
		return "🔎 Searching content..."
	case "WebSearch":
		return "🌐 Searching web..."
	case "WebFetch":
		return "🌐 Fetching page..."
	default:
		return fmt.Sprintf("🔧 %s...", name)
	}
}
