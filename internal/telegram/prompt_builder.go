package telegram

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
)

const (
	maxMemoryFileChars  = 8000
	maxMemoryTotalChars = 40000
)

// buildSystemPrompt assembles all system prompt sections for a request.
func (bc *BotController) buildSystemPrompt(userText string, agent *agents.Agent, chatID int64, messageID int, threadID int) (string, error) {
	var sections []string
	var personaLen, agentLen, cronLen, telegramLen int

	// Runtime identity — tells the model what provider and model it is running on
	provider := bc.config.DefaultProvider
	model := bc.config.DefaultModel
	if agent != nil && agent.Model != "" {
		model = agent.Model
	}
	identitySection := fmt.Sprintf("# Runtime Identity\n\nYou are running via the Aurelia bridge over the PI SDK.\nProvider: %s\nModel: %s\nAlways answer accurately when asked what model you are.", provider, model)
	sections = append(sections, identitySection)

	// Persona prompt
	if bc.persona != nil {
		personaPrompt, err := bc.persona.BuildPrompt()
		if err != nil {
			log.Printf("Persona prompt error (non-fatal): %v", err)
		} else if personaPrompt != "" {
			personaLen = len(personaPrompt)
			sections = append(sections, personaPrompt)
		}
	}

	// Agent-specific prompt
	if agent != nil && agent.Prompt != "" {
		agentSection := "# Agent Instructions\n\n" + agent.Prompt
		agentLen = len(agentSection)
		sections = append(sections, agentSection)
	}

	// Orchestrator TLC methodology — only when user signals planning/implementation intent
	var orchLen int
	if bc.orchestrator != nil && looksLikePlanningIntent(userText) {
		agentSummaries := bc.buildAgentSummaries()
		orchSection := orchestrator.BuildOrchestratorPrompt("", agentSummaries)
		orchLen = len(orchSection)
		sections = append(sections, orchSection)
	}

	// Cron scheduling instructions
	cronSection := bc.buildCronInstructions(chatID)
	cronLen = len(cronSection)
	sections = append(sections, cronSection)

	// Telegram interaction instructions
	telegramSection := bc.buildTelegramInstructions(chatID, messageID, threadID, agent)
	telegramLen = len(telegramSection)
	sections = append(sections, telegramSection)

	// Auto-memory instructions (SDK auto-memory doesn't activate via programmatic API,
	// so we instruct the model explicitly)
	memorySection := bc.buildMemoryInstructions(chatID, threadID, agent)
	sections = append(sections, memorySection)

	// Project docs (CLAUDE.md / AGENTS.md) when cwd is set
	if projectSection := bc.buildProjectDocsSection(chatID, agent, threadID); projectSection != "" {
		sections = append(sections, projectSection)
	}

	result := strings.Join(sections, "\n\n")
	log.Printf("system prompt breakdown: persona=%d agent=%d orch=%d cron=%d telegram=%d memory=%d total=%d chars",
		personaLen, agentLen, orchLen, cronLen, telegramLen, len(memorySection), len(result))

	return result, nil
}

// effectiveCwd resolves the working directory: agent override → chat-level.
func (bc *BotController) effectiveCwd(agent *agents.Agent, chatID int64, threadID int) string {
	if agent != nil && agent.Cwd != "" {
		return agent.Cwd
	}
	return bc.sessions.GetCwd(chatID, threadID)
}

// buildTelegramInstructions returns instructions for interacting with the Telegram chat.
func (bc *BotController) buildTelegramInstructions(chatID int64, messageID int, threadID int, agent *agents.Agent) string {
	bin := "aurelia"
	if bc.exePath != "" {
		bin = bc.exePath
	}

	forumContext := ""
	if threadID > 0 {
		forumContext = fmt.Sprintf("\nYou are in a FORUM TOPIC (thread ID %d).\nMessages and replies are scoped to this topic — they do NOT leak to other topics.\nThe typing indicator and all responses will appear in THIS topic only.\n", threadID)
	}

	return fmt.Sprintf(`## Telegram Context

You ARE the Telegram bot. The user is talking to you via Telegram chat %d.
The current message ID is %d.`+forumContext+`

You can interact with the chat using the Aurelia CLI via Bash:

React to a message with emoji:
`+"`%s telegram react %d %d <emoji>`"+`

Available emojis: 👍 👎 ❤️ 🔥 🎉 🤩 😱 😁 😢 💩 🤮 🥰 🤯 🤔 🤬 👏 🙏 👌 😍 💯 ⚡️ 🏆

Use reactions naturally and contextually — react when it adds to the conversation, not on every message.
DO NOT use the Telegram MCP plugin for reactions or replies — use the Aurelia CLI above.

### Working directory
Current working directory: %s

IMPORTANT rules about the working directory:
- When cwd is "(none)", you are in CHAT MODE. Do NOT read files, run commands, or analyze any project. Only use your memory and conversation context to answer questions.
- When the user wants to work on files or a project, tell them to set the directory first: `+"`/cwd <path>`"+`
- Only perform file operations (Read, Write, Edit, Bash, Glob, Grep) when a cwd is explicitly set.
- If the user asks about "this project" or "the project", refer to conversation context and memory — do NOT go reading random directories.`,
		chatID, messageID, bin, chatID, messageID, func() string {
			if cwd := bc.effectiveCwd(agent, chatID, threadID); cwd != "" {
				return cwd
			}
			return "(none — no project set)"
		}())
}

// topicMemoryDir returns the directory used to store memories scoped to a
// forum topic. Empty when threadID is 0 (private chats / non-forum groups).
func topicMemoryDir(memoryDir string, chatID int64, threadID int) string {
	if threadID <= 0 {
		return ""
	}
	return filepath.Join(memoryDir, "topics", fmt.Sprintf("chat_%d", chatID), fmt.Sprintf("thread_%d", threadID))
}

// buildMemoryInstructions returns the system prompt section for persistent memory.
func (bc *BotController) buildMemoryInstructions(chatID int64, threadID int, agent *agents.Agent) string {
	var sb strings.Builder

	cwd := bc.effectiveCwd(agent, chatID, threadID)
	hasProject := cwd != "" && bc.resolver != nil

	topicSuffix := ""
	if threadID > 0 {
		topicSuffix = fmt.Sprintf(" (topic %d)", threadID)
	}

	sb.WriteString(fmt.Sprintf(`## Persistent Memory — YOU HAVE MEMORY

IMPORTANT: Unlike standard coding agents, you DO have persistent memory across conversations. Your memory contents are loaded below. NEVER say you "don't have memory" or that "each session starts from zero" — that is FALSE. Always check your memory contents below before answering questions about past conversations.`))

	// Saving instructions depend on whether project context is active
	topicDir := topicMemoryDir(bc.memoryDir, chatID, threadID)

	if hasProject {
		projectDir := bc.resolver.ProjectMemoryDir(cwd)
		teamDir := bc.resolver.ProjectTeamMemoryDir(cwd)
		projectName := filepath.Base(cwd)

		sb.WriteString("\n\n### Memory Layers" + topicSuffix + "\n\n")
		sb.WriteString("Save each fact in the correct layer:\n\n")
		sb.WriteString("| Layer | Directory | What to save |\n")
		sb.WriteString("|---|---|---|\n")
		sb.WriteString("| **Global** | " + bc.memoryDir + " | Personal facts, preferences, communication style — applies across all projects |\n")
		sb.WriteString("| **Project Private** | " + projectDir + " | Your personal notes, work log, individual decisions for project \"" + projectName + "\" |\n")
		sb.WriteString("| **Project Team** | " + teamDir + " | Stack, conventions, architecture, known bugs — useful for any team member on \"" + projectName + "\" |\n")
		if topicDir != "" {
			sb.WriteString("| **Topic** | " + topicDir + " | Facts specific to this forum topic — isolated from other topics |\n")
		}

		sb.WriteString("\n### Saving memory\n")
		sb.WriteString("When something meaningful happens, save it using the Write tool to the correct layer:\n")
		sb.WriteString("1. Write/update a topic file in the appropriate directory\n")
		sb.WriteString("2. Update the MEMORY.md index in that directory: one line per file as - [Title](file.md) — summary\n\n")
		sb.WriteString("Do NOT just promise to save — actually call Write before your response ends.")
	} else {
		dirList := bc.memoryDir
		if topicDir != "" {
			dirList += " (global) and " + topicDir + " (this topic)"
		}

		sb.WriteString("\n\n### Saving memory" + topicSuffix + "\n")
		sb.WriteString("When something meaningful happens (project work, decisions, personal facts, preferences), save it using the Write tool:\n")
		sb.WriteString("1. Write/update a topic file in " + dirList + "/\n")
		sb.WriteString("2. Update MEMORY.md index: one line per file as - [Title](file.md) — summary\n\n")
		sb.WriteString("Do NOT just promise to save — actually call Write before your response ends.")
	}

	sb.WriteString(`

### Forgetting/removing memories
If the user asks to forget or remove something, delete the file with Bash (rm) and remove its entry from MEMORY.md. Do NOT just say you removed it — actually delete the file.

### Project configuration files
Never mention internal project files (CLAUDE.md, AGENTS.md) in casual conversation. Only reference them when a working directory is set AND the user asks about project configuration.`)

	// Inject actual memory contents
	memoryContent := bc.loadMemoryContents(chatID, threadID, agent)
	if memoryContent != "" {
		sb.WriteString("\n\n### Current Memory Contents" + topicSuffix + "\n\n")
		sb.WriteString(memoryContent)
	}

	return sb.String()
}

// loadMemoryContents reads memory files from all available layers and returns
// their contents for injection into the system prompt.
// Total output is capped at maxMemoryTotalChars; layers beyond the cap are skipped.
func (bc *BotController) loadMemoryContents(chatID int64, threadID int, agent *agents.Agent) string {
	var sb strings.Builder
	var total int

	appendLayer := func(header, content string) {
		if content == "" || total >= maxMemoryTotalChars {
			return
		}
		remaining := maxMemoryTotalChars - total - len(header)
		if remaining <= 0 {
			log.Printf("memory: skipped layer (%d chars total, max %d)", total, maxMemoryTotalChars)
			return
		}
		if len(content) > remaining {
			content = truncateToBudget(content, remaining)
			log.Printf("memory: truncated layer to fit total budget (%d chars max)", maxMemoryTotalChars)
		}
		sb.WriteString(header)
		sb.WriteString(content)
		total += len(header) + len(content)
	}

	appendLayer("#### Global (cross-project)\n\n", bc.loadMemoryDir(bc.memoryDir))

	if topicDir := topicMemoryDir(bc.memoryDir, chatID, threadID); topicDir != "" {
		header := fmt.Sprintf("\n\n#### Topic %d (chat %d)\n\n", threadID, chatID)
		appendLayer(header, bc.loadMemoryDir(topicDir))
	}

	cwd := bc.effectiveCwd(agent, chatID, threadID)
	if cwd != "" && bc.resolver != nil {
		projectName := filepath.Base(cwd)

		header := fmt.Sprintf("\n\n#### Project: %s (private)\n\n", projectName)
		appendLayer(header, bc.loadMemoryDir(bc.resolver.ProjectMemoryDir(cwd)))

		header = fmt.Sprintf("\n\n#### Project: %s (team)\n\n", projectName)
		appendLayer(header, bc.loadMemoryDir(bc.resolver.ProjectTeamMemoryDir(cwd)))
	}

	return sb.String()
}

// loadMemoryDir reads MEMORY.md and all .md files from a directory.
// Results are cached by mtime to avoid redundant disk reads.
func (bc *BotController) loadMemoryDir(dir string) string {
	if dir == "" {
		return ""
	}

	if bc.memoryCache != nil {
		if cached, ok := bc.memoryCache.get(dir); ok {
			return cached
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	mtimes := make(map[string]time.Time, len(entries))

	indexPath := filepath.Join(dir, "MEMORY.md")
	if indexData, err := os.ReadFile(indexPath); err == nil && len(indexData) > 0 {
		sb.WriteString("**MEMORY.md (index):**\n")
		sb.WriteString(truncateContent(string(indexData), "MEMORY.md"))
	}
	if fi, err := os.Stat(indexPath); err == nil {
		mtimes["MEMORY.md"] = fi.ModTime()
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "MEMORY.md" || !strings.HasSuffix(name, ".md") {
			continue
		}
		if fi, err := entry.Info(); err == nil {
			mtimes[name] = fi.ModTime()
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(data) == 0 {
			continue
		}
		content := truncateContent(strings.TrimSpace(string(data)), name)
		fmt.Fprintf(&sb, "\n\n**%s:**\n%s", name, content)
	}

	content := sb.String()
	if bc.memoryCache != nil {
		bc.memoryCache.put(dir, content, mtimes)
	}
	return content
}

// truncateContent truncates content to maxMemoryFileChars and appends a notice.
func truncateContent(content, filename string) string {
	if len(content) <= maxMemoryFileChars {
		return content
	}
	log.Printf("memory: truncated %s (%d chars, max %d)", filename, len(content), maxMemoryFileChars)
	return content[:maxMemoryFileChars] + "\n\n[...truncado, ver arquivo completo via Read tool]"
}

func truncateToBudget(content string, budget int) string {
	if len(content) <= budget {
		return content
	}
	if budget <= 0 {
		return ""
	}
	notice := "\n\n[...memória truncada por limite total]"
	if budget <= len(notice) {
		return content[:budget]
	}
	return content[:budget-len(notice)] + notice
}

// buildProjectDocsSection reads CLAUDE.md and AGENTS.md from the active cwd.
func (bc *BotController) buildProjectDocsSection(chatID int64, agent *agents.Agent, threadID int) string {
	cwd := ""
	if agent != nil && agent.Cwd != "" {
		cwd = agent.Cwd
	}
	if cwd == "" {
		cwd = bc.sessions.GetCwd(chatID, threadID)
	}
	if cwd == "" {
		return ""
	}

	var parts []string

	claudeMd, err := os.ReadFile(filepath.Join(cwd, "CLAUDE.md"))
	if err == nil && len(claudeMd) > 0 {
		parts = append(parts, fmt.Sprintf("# Project Instructions (CLAUDE.md)\n\n%s", strings.TrimSpace(string(claudeMd))))
	}

	agentsMd, err := os.ReadFile(filepath.Join(cwd, "AGENTS.md"))
	if err == nil && len(agentsMd) > 0 {
		parts = append(parts, fmt.Sprintf("# Squad Configuration (AGENTS.md)\n\n%s", strings.TrimSpace(string(agentsMd))))
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// buildCronInstructions returns the system prompt section for cron scheduling.
func (bc *BotController) buildCronInstructions(chatID int64) string {
	bin := "aurelia"
	if bc.exePath != "" {
		bin = bc.exePath
	}
	chatFlag := fmt.Sprintf("--chat-id %d", chatID)

	return fmt.Sprintf(`## Scheduling Tasks

Use the Aurelia cron CLI for ALL scheduling. Internal scheduling tools die with the session — only the CLI persists.

- Recurring: `+"`%s cron add \"<cron-expr>\" \"<prompt>\" %s`"+`
- One-time: `+"`%s cron once \"<ISO-timestamp>\" \"<prompt>\" %s`"+`
- List: `+"`%s cron list %s`"+` | Delete: `+"`%s cron del <id>`"+` | Pause/Resume: `+"`%s cron pause|resume <id>`"+`

Cron prompts are ACTION instructions (not content). They run in isolated sessions with no history. The --chat-id flag is required.`,
		bin, chatFlag,
		bin, chatFlag,
		bin, chatFlag,
		bin, bin,
	)
}
