package dream

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kocar/aurelia/internal/bridge"
	"github.com/kocar/aurelia/internal/session"
)

// AfterTurnNudge checks if enough turns have accumulated to trigger a nudge review.
// It runs in background without blocking the chat.
func (d *Dreamer) AfterTurnNudge(chatID int64, cwd string, buffer *session.NudgeBuffer) {
	if !d.config.NudgeEnabled || buffer == nil {
		return
	}

	if buffer.TurnCount(chatID) < d.config.NudgeTurns {
		return
	}

	d.flushNudgeBuffer(chatID, cwd, buffer)
}

// FlushNudge forces a nudge review with whatever is in the buffer, regardless
// of the turn threshold. Call this on session reset (/new, auto-reset) so
// short conversations are not lost.
func (d *Dreamer) FlushNudge(chatID int64, cwd string, buffer *session.NudgeBuffer) {
	if !d.config.NudgeEnabled || buffer == nil {
		return
	}
	if buffer.TurnCount(chatID) == 0 {
		return
	}
	d.flushNudgeBuffer(chatID, cwd, buffer)
}

func (d *Dreamer) flushNudgeBuffer(chatID int64, cwd string, buffer *session.NudgeBuffer) {
	// Prevent concurrent nudges
	if !d.nudgeRunning.CompareAndSwap(false, true) {
		return
	}

	messages := buffer.GetAndReset(chatID)
	if len(messages) == 0 {
		d.nudgeRunning.Store(false)
		return
	}

	go d.runNudge(messages, cwd)
}

func (d *Dreamer) runNudge(messages []session.NudgeMessage, cwd string) {
	defer d.nudgeRunning.Store(false)

	log.Printf("[nudge] starting review with %d messages...", len(messages))
	start := time.Now()

	// Build conversation transcript
	var transcript strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&transcript, "**%s:** %s\n\n", m.Role, m.Content)
	}

	// Build system prompt with memory directories
	sysPrompt := d.buildNudgePrompt(cwd)

	prompt := fmt.Sprintf("Review this conversation and save important information to the correct memory layer.\n\n## Conversation\n\n%s", transcript.String())

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
			Model:          model,
			SystemPrompt:   sysPrompt,
			Cwd:            d.memoryDir,
			MaxTurns:       15,
			PermissionMode: "bypassPermissions",
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

func (d *Dreamer) buildNudgePrompt(cwd string) string {
	// When no project context, use global-only prompt
	if cwd == "" || d.resolver == nil {
		return `You are a memory review agent. Save important information from the conversation.

Memory directory: ` + d.memoryDir + `

## What to save
- Facts the user revealed (name, preferences, workflow habits)
- Decisions made or preferences expressed
- Work completed and current state
- Problems encountered and solutions found

## How to save
1. Read MEMORY.md to see existing files
2. Update existing files or create new ones — one fact per line
3. Update MEMORY.md index if needed

## Rules
- Update existing facts, don't duplicate
- Delete files with Bash (rm) if user asks to forget
- Do NOT save conversation transcripts or code
- Do NOT modify anything in personas/ subdirectory
- If nothing worth saving, make no changes`
	}

	globalDir := d.memoryDir
	projectDir := d.resolver.ProjectMemoryDir(cwd)
	teamDir := d.resolver.ProjectTeamMemoryDir(cwd)

	return `You are a memory review agent. Save important information to the correct layer.

## Directory mapping
- GLOBAL = ` + globalDir + `
- PROJECT = ` + projectDir + `
- TEAM = ` + teamDir + `

## Where to save
GLOBAL — personal facts, preferences, language, hobbies (cross-project)
TEAM — stack, conventions, architecture, bugs, workarounds (shared with team)
PROJECT — personal notes, work log, task state (only you)

## How to save
1. Read MEMORY.md in the target directory
2. Update existing files or create new ones — one fact per line
3. Update MEMORY.md index if needed

## Rules
- Use ALL 3 directories — classify each fact into the right one
- Update existing facts, don't duplicate
- Delete files with Bash (rm) if user asks to forget
- Do NOT save conversation transcripts or code
- Do NOT modify anything in personas/ subdirectory
- If nothing worth saving, make no changes`
}
