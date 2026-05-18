package dream

import (
	"context"
	"embed"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/session"
)

//go:embed prompts/nudge_global.tmpl prompts/nudge_project.tmpl
var nudgeTemplateFS embed.FS

// nudgeTemplateData holds the data for rendering nudge prompt templates.
type nudgeTemplateData struct {
	GlobalDir  string
	TopicDir   string
	ProjectDir string
	TeamDir    string
}

// AfterTurnNudge checks if enough turns have accumulated to trigger a nudge review.
// It runs in background without blocking the chat.
func (d *Dreamer) AfterTurnNudge(chatID int64, threadID int, cwd string, buffer *session.NudgeBuffer) {
	if !d.config.NudgeEnabled || buffer == nil {
		return
	}

	if buffer.TurnCount(chatID, threadID) < d.config.NudgeTurns {
		return
	}

	d.flushNudgeBuffer(chatID, threadID, cwd, buffer)
}

// FlushNudge forces a nudge review with whatever is in the buffer, regardless
// of the turn threshold. Call this on session reset (/new, auto-reset) so
// short conversations are not lost.
func (d *Dreamer) FlushNudge(chatID int64, threadID int, cwd string, buffer *session.NudgeBuffer) {
	if !d.config.NudgeEnabled || buffer == nil {
		return
	}
	if buffer.TurnCount(chatID, threadID) == 0 {
		return
	}
	d.flushNudgeBuffer(chatID, threadID, cwd, buffer)
}

func (d *Dreamer) flushNudgeBuffer(chatID int64, threadID int, cwd string, buffer *session.NudgeBuffer) {
	// Prevent concurrent nudges
	if !d.nudgeRunning.CompareAndSwap(false, true) {
		return
	}

	messages := buffer.GetAndReset(chatID, threadID)
	if len(messages) == 0 {
		d.nudgeRunning.Store(false)
		return
	}

	go d.runNudge(messages, chatID, threadID, cwd)
}

func (d *Dreamer) runNudge(messages []session.NudgeMessage, chatID int64, threadID int, cwd string) {
	defer d.nudgeRunning.Store(false)

	log.Printf("[nudge] starting review with %d messages...", len(messages))
	start := time.Now()

	// Build conversation transcript
	var transcript strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&transcript, "**%s:** %s\n\n", m.Role, m.Content)
	}

	// Build system prompt with memory directories
	sysPrompt := d.buildNudgePrompt(cwd, chatID, threadID)

	prompt := fmt.Sprintf(`TASK: Extract facts from the conversation below and save them using the Write tool.

STEP 1: Read the conversation.
STEP 2: List facts worth remembering (user preferences, decisions, topics discussed, work done, plans mentioned).
STEP 3: For EACH fact, call the Write tool to save it. Use one file per topic (e.g. conversation_topics.md, user_preferences.md).
STEP 4: If MEMORY.md exists, update it with an index entry for each file you created.

IMPORTANT: You MUST call the Write tool at least once. If the conversation has any content at all, there is something worth saving — at minimum, what topics were discussed.

## Conversation

%s`, transcript.String())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	model := d.config.NudgeModel
	if model == "" {
		model = d.config.ExtractModel
	}

	req := bridge.Request{
		Command: "query",
		Prompt:  prompt,
		Options: bridge.RequestOptions{
			Provider:       d.config.Provider,
			Model:          model,
			SystemPrompt:   sysPrompt,
			Cwd:            d.memoryDir,
			AllowedTools:   []string{"Read", "Glob", "Grep", "Write", "Edit", "Bash"},
			NoUserSettings: true,
			PersistSession: boolPtr(false),
		},
	}

	ev, err := d.bridge.ExecuteSync(ctx, req)
	if err != nil {
		log.Printf("[nudge] failed: %v", err)
		return
	}
	if ev.Type == "error" {
		log.Printf("[nudge] error: %s", ev.Message)
		return
	}

	log.Printf("[nudge] completed in %s — cost=$%.4f turns=%d",
		time.Since(start).Round(time.Second), ev.CostUSD, ev.NumTurns)
}

func (d *Dreamer) buildNudgePrompt(cwd string, chatID int64, threadID int) string {
	globalDir := d.memoryDir
	topicDir := ""
	if threadID > 0 {
		topicDir = filepath.Join(globalDir, "topics", fmt.Sprintf("chat_%d", chatID), fmt.Sprintf("thread_%d", threadID))
	}

	data := nudgeTemplateData{
		GlobalDir: globalDir,
		TopicDir:  topicDir,
	}

	// When no project context, use global-only template
	if cwd == "" || d.resolver == nil {
		tmpl := template.Must(template.New("nudge_global").ParseFS(nudgeTemplateFS, "prompts/nudge_global.tmpl"))
		var buf strings.Builder
		if err := tmpl.ExecuteTemplate(&buf, "nudge_global.tmpl", data); err != nil {
			log.Printf("[nudge] template error: %v", err)
			return ""
		}
		return buf.String()
	}

	// Project context
	data.ProjectDir = d.resolver.ConversationProjectMemoryDir(cwd, chatID, threadID)
	data.TeamDir = d.resolver.ProjectTeamMemoryDir(cwd)

	tmpl := template.Must(template.New("nudge_project").ParseFS(nudgeTemplateFS, "prompts/nudge_project.tmpl"))
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "nudge_project.tmpl", data); err != nil {
		log.Printf("[nudge] template error: %v", err)
		return ""
	}
	return buf.String()
}
