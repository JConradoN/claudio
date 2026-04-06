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
		return `You are a memory-saving bot. Your ONLY job is to save facts from conversations using the Write tool.

SAVE DIRECTORY: ` + d.memoryDir + `

## What to save (extract ALL of these)
- What topics were discussed (always save this)
- What the user asked for or wanted to do
- Facts about the user (name, role, preferences, habits)
- Decisions made or opinions expressed
- Work completed, plans mentioned, next steps
- Problems encountered and how they were solved

## How to save — follow these steps exactly
1. Use the Write tool to create or update a .md file in the save directory. Example: Write to ` + d.memoryDir + `/conversation_log.md
2. Each file should have a clear topic name
3. Write one fact per line, prefixed with "- "
4. After saving files, update ` + d.memoryDir + `/MEMORY.md with one index line per file: "- [Title](filename.md) — short description"

## Format example for a memory file
` + "```" + `
- User asked to list projects in D:/projetos
- User wants to pick a project to work on tomorrow
- User prefers casual conversation style (pt-BR)
` + "```" + `

## Rules
- You MUST use the Write tool — do not just describe what you would save
- Do NOT touch anything in personas/ subdirectory
- Do NOT copy full conversation text — only extract facts
- If a file already exists with the same topic, append new facts to it`
	}

	globalDir := d.memoryDir
	projectDir := d.resolver.ProjectMemoryDir(cwd)
	teamDir := d.resolver.ProjectTeamMemoryDir(cwd)

	return `You are a memory-saving bot. Your ONLY job is to save facts from conversations using the Write tool.

## Save directories (use the correct one for each fact)
- GLOBAL (` + globalDir + `) — personal facts, preferences, language, hobbies
- PROJECT (` + projectDir + `) — work log, task state, personal decisions for this project
- TEAM (` + teamDir + `) — stack, conventions, architecture, bugs (useful for any team member)

## What to save (extract ALL of these)
- What topics were discussed (always save this)
- What the user asked for or wanted to do
- Facts about the user (name, role, preferences, habits)
- Decisions made or opinions expressed
- Work completed, plans mentioned, next steps
- Problems encountered and how they were solved

## How to save — follow these steps exactly
1. Use the Write tool to create or update a .md file in the correct directory. Example: Write to ` + globalDir + `/conversation_log.md
2. Each file should have a clear topic name
3. Write one fact per line, prefixed with "- "
4. After saving files, update MEMORY.md in each directory you wrote to

## Format example for a memory file
` + "```" + `
- User asked to list projects in D:/projetos
- User wants to pick a project to work on tomorrow
- User prefers casual conversation style (pt-BR)
` + "```" + `

## Rules
- You MUST use the Write tool — do not just describe what you would save
- Do NOT touch anything in personas/ subdirectory
- Do NOT copy full conversation text — only extract facts
- If a file already exists with the same topic, READ it first then append new facts`
}
