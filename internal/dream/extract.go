package dream

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kocar/aurelia/internal/bridge"
)

const extractionPrompt = `You are a memory extraction agent. Analyze a conversation snippet and update memory files with any new information.

Memory directory: the current working directory.

## What to extract
- Work completed (files changed, features added, tasks done)
- State changes (project status, task progress)
- Decisions made or preferences expressed
- Personal facts the user shared
- Problems encountered and solutions found

## How to save
1. Read MEMORY.md to see existing files
2. Read relevant existing memory files that may need updating
3. Update files with new information — replace outdated facts with current ones
4. Update MEMORY.md index if needed

Memory file format:
` + "```" + `
---
name: Topic Name
description: One-line description
type: user|feedback|project|reference
---
Content here. One fact per line.
` + "```" + `

## Removing memories
If the user asked to forget or remove something, DELETE the memory file with Bash (rm) and remove its entry from MEMORY.md.

## Rules
- When something changed, update the existing fact — don't keep old and new side by side
- When the user asks to forget/remove something, actually delete the file
- Be concise — one fact per line
- Update existing files rather than creating duplicates
- Do NOT save conversation transcripts or code
- Do NOT modify anything in personas/ subdirectory
- If nothing worth saving happened, make no changes
`

// ExtractMemories runs a background agent that analyzes the last user interaction
// and saves relevant information to memory files.
func (d *Dreamer) ExtractMemories(userMessage string, assistantResponse string) {
	if !d.config.Enabled || d.memoryDir == "" {
		return
	}

	go d.runExtraction(userMessage, assistantResponse)
}

func (d *Dreamer) runExtraction(userMessage string, assistantResponse string) {
	// Truncate to avoid huge prompts but keep enough for context
	if len(userMessage) > 1000 {
		userMessage = userMessage[:1000] + "..."
	}
	if len(assistantResponse) > 4000 {
		assistantResponse = assistantResponse[:4000] + "..."
	}

	prompt := fmt.Sprintf("Analyze this conversation and save any important information to memory.\n\n**User:** %s\n\n**Assistant:** %s",
		userMessage, assistantResponse)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	req := bridge.Request{
		Command: "query",
		Prompt:  prompt,
		Options: bridge.RequestOptions{
			Model:          d.config.ExtractModel,
			SystemPrompt:   extractionPrompt,
			Cwd:            d.memoryDir,
			MaxTurns:       10,
			PermissionMode: "bypassPermissions",
			AllowedTools:   []string{"Read", "Glob", "Grep", "Write", "Edit", "Bash"},
			NoUserSettings: true,
			PersistSession: boolPtr(false),
		},
	}

	ev, err := d.bridge.ExecuteSync(ctx, req)
	if err != nil {
		log.Printf("[memory-extract] failed: %v", err)
		return
	}
	if ev.Type == "error" {
		log.Printf("[memory-extract] error: %s", ev.Message)
		return
	}

	log.Printf("[memory-extract] done — cost=$%.4f turns=%d", ev.CostUSD, ev.NumTurns)
}
