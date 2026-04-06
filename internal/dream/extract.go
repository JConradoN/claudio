package dream

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kocar/aurelia/internal/bridge"
)

const extractionPromptGlobalOnly = `You are a memory extraction agent. Analyze a conversation snippet and update memory files with any new information.

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

// buildExtractionPrompt returns the extraction system prompt.
// When cwd is set, it includes instructions to classify facts into 3 layers.
// Uses short aliases (GLOBAL, PROJECT, TEAM) with a path mapping to keep things clear.
func buildExtractionPrompt(globalDir, projectDir, teamDir string) string {
	return `You are a memory extraction agent. Save information to the correct memory layer.

## Directory mapping
- GLOBAL = ` + globalDir + `
- PROJECT = ` + projectDir + `
- TEAM = ` + teamDir + `

## Where to save each type of fact

GLOBAL — personal facts, preferences, language, hobbies (cross-project)
TEAM — stack, conventions, architecture, bugs, workarounds (shared with team)
PROJECT — personal notes, work log, task state (only you)

## How to save
1. Read MEMORY.md in the target directory
2. Update existing files or create new ones — one fact per line
3. Update MEMORY.md index if needed

File format:
` + "```" + `
---
name: Topic Name
description: One-line description
type: user|feedback|project|reference
---
Content here. One fact per line.
` + "```" + `

## Rules
- Use ALL 3 directories — classify each fact into the right one
- Update existing facts, don't duplicate
- Delete files with Bash (rm) if user asks to forget
- Do NOT save conversation transcripts or code
- Do NOT modify anything in personas/ subdirectory
- If nothing worth saving, make no changes
`
}

// ExtractMemories runs a background agent that analyzes the last user interaction
// and saves relevant information to memory files.
// When cwd is non-empty and a resolver is available, facts are classified into
// global, project-private, and project-team layers.
func (d *Dreamer) ExtractMemories(userMessage string, assistantResponse string, cwd string) {
	if !d.config.Enabled || d.memoryDir == "" {
		return
	}

	go d.runExtraction(userMessage, assistantResponse, cwd)
}

func (d *Dreamer) runExtraction(userMessage string, assistantResponse string, cwd string) {
	// Truncate to avoid huge prompts but keep enough for context
	if len(userMessage) > 1000 {
		userMessage = userMessage[:1000] + "..."
	}
	if len(assistantResponse) > 4000 {
		assistantResponse = assistantResponse[:4000] + "..."
	}

	prompt := fmt.Sprintf("Analyze this conversation and save any important information to memory.\n\n**User:** %s\n\n**Assistant:** %s",
		userMessage, assistantResponse)

	// Determine system prompt and cwd based on whether project context is available
	sysPrompt := extractionPromptGlobalOnly
	extractCwd := d.memoryDir

	if cwd != "" && d.resolver != nil {
		projectDir := d.resolver.ProjectMemoryDir(cwd)
		teamDir := d.resolver.ProjectTeamMemoryDir(cwd)
		sysPrompt = buildExtractionPrompt(d.memoryDir, projectDir, teamDir)
		// Use global dir as cwd so agent can access all 3 directories via absolute paths
		extractCwd = d.memoryDir
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	req := bridge.Request{
		Command: "query",
		Prompt:  prompt,
		Options: bridge.RequestOptions{
			Model:          d.config.ExtractModel,
			SystemPrompt:   sysPrompt,
			Cwd:            extractCwd,
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
