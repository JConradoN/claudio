package telegram

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

func (bc *BotController) processInput(c telebot.Context, text string) error {
	return bc.processInputWithImages(c, text, nil)
}

func (bc *BotController) processInputWithImages(c telebot.Context, text string, images []bridge.ImageAttachment) error {
	if state, ok := bc.popPendingBootstrap(c.Sender().ID); ok {
		switch state.Step {
		case bootstrapStepAssistant:
			return bc.completeBootstrapAssistant(c, state, text)
		default:
			return bc.completeBootstrapProfile(c, state, text)
		}
	}

	// 0. Extract thread ID for forum topic support (0 = no topic / private)
	threadID := c.Message().ThreadID

	// 0. Command layer — intercept system commands before LLM
	if cmd := MatchCommand(text); cmd != nil {
		return bc.handleCommand(c, cmd)
	}

	// 1. Route to agent (sync — fast)
	agent := bc.routeAgent(text)

	// Strip @agent prefix from user text if agent was routed
	userText := text
	if agent != nil {
		if idx := strings.IndexByte(text[1:], ' '); idx != -1 {
			userText = strings.TrimSpace(text[idx+2:])
		} else {
			userText = ""
		}
	}

	if userText == "" {
		userText = text
	}

	// 1b. Auto-detect project path — only on new sessions (no active session)
	// Changing cwd mid-session breaks SDK continue (different project = different session)
	chatID := c.Chat().ID
	if _, active := bc.sessions.GetWithState(chatID, threadID); !active {
		if detected := bc.detectProjectPath(userText); detected != "" {
			bc.sessions.SetCwd(chatID, threadID, detected)
			log.Printf("cwd: auto-detected %s for chat=%d thread=%d", detected, chatID, threadID)
			if bc.resolver != nil {
				if err := runtime.BootstrapProjectMemory(bc.resolver, detected); err != nil {
					log.Printf("cwd: failed to bootstrap project memory for %s: %v", detected, err)
				}
			}
		}
	}

	// 2. Build system prompt (sync — fast)
	messageID := c.Message().ID
	systemPrompt, err := bc.buildSystemPrompt(userText, agent, chatID, messageID, threadID)
	if err != nil {
		log.Printf("Failed to build system prompt: %v", err)
		return SendError(bc.bot, c.Chat(), "Falha ao montar o prompt de sistema.")
	}

	// 3. Build bridge request (sync)
	req := bc.buildBridgeRequest(userText, systemPrompt, agent, chatID, threadID)
	req.Options.Images = images

	// 3b. Vision fallback: if images are present and a vision model is configured,
	// override the model/provider so non-vision models can delegate image analysis.
	if len(images) > 0 {
		if vModel, vProvider := bc.config.VisionFallback(); vModel != "" {
			log.Printf("vision: switching to fallback model %s/%s for image input", vProvider, vModel)
			req.Options.Model = vModel
			if vProvider != "" {
				req.Options.Provider = vProvider
			}
		} else {
			log.Printf("vision: no fallback configured, using default model")
		}
	}

	// 4. Launch async execution — don't block the handler
	go bc.executeAsync(chatID, threadID, messageID, req, userText)

	return nil
}

const (
	classifyTimeout         = 15 * time.Second
	typingIndicatorInterval = 4 * time.Second
	bridgeExecutionTimeout  = 10 * time.Minute
)

// routeAgent resolves which agent should handle the message, first by @name
// prefix, then by LLM classification if agents are configured.
func (bc *BotController) routeAgent(text string) *agents.Agent {
	agent := bc.agents.Route(text)
	if agent != nil {
		return agent
	}
	if bc.agents == nil {
		return nil
	}
	classifyCtx, classifyCancel := context.WithTimeout(context.Background(), classifyTimeout)
	defer classifyCancel()
	return bc.agents.Classify(classifyCtx, text, bc.classifyFunc())
}

func (bc *BotController) classifyFunc() agents.ClassifyFunc {
	return func(ctx context.Context, system, prompt string) (string, error) {
		result, err := bc.bridge.ExecuteSync(ctx, bridge.Request{
			Command: "query",
			Prompt:  prompt,
			Options: bridge.RequestOptions{
				Provider:       bc.config.DefaultProvider,
				Model:          bc.config.DefaultModel,
				SystemPrompt:   system,
				MaxTurns:       1,
				PermissionMode: "bypassPermissions",
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
func (bc *BotController) buildBridgeRequest(userText, systemPrompt string, agent *agents.Agent, chatID int64, threadID int) bridge.Request {
	req := bridge.Request{
		Command: "query",
		Prompt:  userText,
		Options: bridge.RequestOptions{
			Provider:       bc.config.DefaultProvider,
			Model:          bc.config.DefaultModel,
			SystemPrompt:   systemPrompt,
			MaxTurns:       bc.config.MaxIterations,
			PermissionMode: "bypassPermissions",
			DisabledTools:  bridge.TelegramPluginTools,
		},
	}

	if agent != nil {
		if agent.Model != "" {
			req.Options.Model = agent.Model
		}
		if agent.Cwd != "" {
			req.Options.Cwd = agent.Cwd
		}
		if len(agent.MCPServers) > 0 {
			req.Options.MCPServers = agent.MCPServers
		}
		if len(agent.AllowedTools) > 0 {
			req.Options.AllowedTools = agent.AllowedTools
		}
	}

	// Pass all agents to SDK for native delegation
	if sdkAgents := agents.BuildSDKAgents(bc.agents); sdkAgents != nil {
		req.Options.Agents = sdkAgents
	}

	// PI resumes sessions by ID/path. Always pass the stored session ID so
	// the Bridge can reuse warm sessions or reopen persisted ones after restart.
	if sessionID, active := bc.sessions.GetWithState(chatID, threadID); sessionID != "" {
		req.Options.Resume = sessionID
		if active {
			log.Printf("session: chat=%d thread=%d mode=continue sid=%s (len=%d)", chatID, threadID, sessionID[:8], len(sessionID))
		} else {
			log.Printf("session: chat=%d thread=%d mode=resume sid=%s (len=%d)", chatID, threadID, sessionID[:8], len(sessionID))
		}
	} else {
		log.Printf("session: chat=%d thread=%d mode=new", chatID, threadID)
	}

	// Apply chat-level cwd if no agent overrides it
	if req.Options.Cwd == "" {
		if chatCwd := bc.sessions.GetCwd(chatID, threadID); chatCwd != "" {
			req.Options.Cwd = chatCwd
		}
	}

	return req
}

// bridgeOutcome indicates how processBridgeEventsAsync terminated.
type bridgeOutcome int

const (
	outcomeSuccess      bridgeOutcome = iota // terminal "result" event
	outcomeLLMError                          // terminal "error" event
	outcomeProcessDeath                      // channel closed without terminal event
)

// bridgeFailureTracker tracks consecutive bridge failures to implement cooldown.
type bridgeFailureTracker struct {
	mu       sync.Mutex
	failures []time.Time // timestamps of recent failures
}

const (
	failureWindowMax = 3                // max failures before cooldown
	failureWindowDur = 1 * time.Minute  // window to count failures
	cooldownDuration = 30 * time.Second // cooldown period after max failures
)

// record adds a failure timestamp and returns true if in cooldown.
func (t *bridgeFailureTracker) record() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.failures = append(t.failures, now)

	// Trim failures outside the window
	cutoff := now.Add(-failureWindowDur)
	start := 0
	for start < len(t.failures) && t.failures[start].Before(cutoff) {
		start++
	}
	t.failures = t.failures[start:]

	return len(t.failures) >= failureWindowMax
}

// inCooldown returns true if we're in cooldown (recent failures >= max).
func (t *bridgeFailureTracker) inCooldown() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.failures) < failureWindowMax {
		return false
	}

	// In cooldown if last failure was within cooldown duration
	last := t.failures[len(t.failures)-1]
	return time.Since(last) < cooldownDuration
}

// reset clears the failure history after a successful execution.
func (t *bridgeFailureTracker) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures = t.failures[:0]
}

// executeAsync runs the bridge execution in a goroutine with its own typing
// indicator and progress reporter. Errors are sent directly to the chat since
// the original handler has already returned.
func (bc *BotController) executeAsync(chatID int64, threadID int, messageID int, req bridge.Request, userText string) {
	chat := &telebot.Chat{ID: chatID}

	// Start typing indicator in the correct thread (0 = general/private)
	stopTyping := startChatActionLoop(bc.bot, chat, telebot.Typing, typingIndicatorInterval, threadID)
	defer stopTyping()

	// Progress reporter (thread-aware for forum topics)
	progress := newProgressReporterWithThread(bc.bot, chat, threadID)
	defer progress.Delete()

	// Execute via bridge
	ctx, cancel := context.WithTimeout(context.Background(), bridgeExecutionTimeout)
	defer cancel()

	ch, err := bc.bridge.Execute(ctx, req)
	if err != nil {
		log.Printf("Bridge execute error: %v", err)
		if err := SendErrorWithThread(bc.bot, chat, "Falha ao conectar com o processador.", threadID); err != nil {
			log.Printf("Failed to send error to chat %d: %v", chat.ID, err)
		}
		return
	}

	// Process events — first attempt
	outcome := bc.processBridgeEventsAsyncWithThread(chat, ch, progress, userText, messageID, threadID)

	if outcome == outcomeSuccess {
		bc.bridgeFailures.reset()
		return
	}
	if outcome != outcomeProcessDeath {
		return
	}

	// --- RETRY PATH: bridge died mid-request ---
	bc.bridgeFailures.record()
	log.Printf("bridge: process died mid-request, retrying for chat=%d thread=%d", chatID, threadID)

	// P3: Check cooldown before retrying
	if bc.bridgeFailures.inCooldown() {
		log.Printf("bridge: in cooldown, skipping retry for chat=%d", chatID)
		_ = SendErrorWithThread(bc.bot, chat, "Processador temporariamente indisponível. Tente novamente em alguns segundos.", threadID)
		return
	}

	// P2: Send reconnection feedback
	var reconnectMsg *telebot.Message
	reconnectMsg, _ = bc.bot.Send(chat, "⚡ Reconectando...", &telebot.SendOptions{ThreadID: threadID})

	retryReq := req
	retryReq.Options.Continue = false
	retryReq.RequestID = ""
	if sid := bc.sessions.Get(chatID, threadID); sid != "" {
		retryReq.Options.Resume = sid
		log.Printf("bridge: retry with resume sid=%s", sid[:8])
	}

	ch, err = bc.bridge.Execute(ctx, retryReq)
	bc.deleteMessage(reconnectMsg) // Bridge restarted (or failed) — remove feedback immediately
	if err != nil {
		log.Printf("bridge: retry failed for chat=%d: %v", chatID, err)
		_ = SendErrorWithThread(bc.bot, chat, "Processador reiniciado mas não conseguiu completar. Tente novamente.", threadID)
		return
	}

	// Second attempt — no more retries
	outcome = bc.processBridgeEventsAsyncWithThread(chat, ch, progress, userText, messageID, threadID)

	switch outcome {
	case outcomeSuccess:
		bc.bridgeFailures.reset()
	case outcomeProcessDeath:
		bc.bridgeFailures.record()
		_ = SendError(bc.bot, chat, "Processador reiniciado mas não conseguiu completar. Tente novamente.")
	}
}

// deleteMessage removes a Telegram message if it exists. Used to clean up
// temporary feedback messages like "Reconectando...".
func (bc *BotController) deleteMessage(msg *telebot.Message) {
	if msg != nil && bc.bot != nil {
		if err := bc.bot.Delete(msg); err != nil {
			log.Printf("Failed to delete reconnect message: %v", err)
		}
	}
}

// processBridgeEventsAsync reads bridge events and sends responses to the
// Telegram chat. Returns the outcome so the caller can decide whether to retry.
func (bc *BotController) processBridgeEventsAsync(chat *telebot.Chat, ch <-chan bridge.Event, progress *progressReporter, userText string, messageID int) bridgeOutcome {
	return bc.processBridgeEventsAsyncWithThread(chat, ch, progress, userText, messageID, 0)
}

func (bc *BotController) processBridgeEventsAsyncWithThread(chat *telebot.Chat, ch <-chan bridge.Event, progress *progressReporter, userText string, messageID int, threadID int) bridgeOutcome {
	var assistantText strings.Builder

	for ev := range ch {
		switch ev.Type {
		case "system":
			if ev.SessionID != "" {
				sidPreview := ev.SessionID
				if len(sidPreview) > 8 {
					sidPreview = sidPreview[:8]
				}
				log.Printf("session store: chat=%d thread=%d sid=%s", chat.ID, threadID, sidPreview)
				bc.sessions.Set(chat.ID, threadID, ev.SessionID)
			}

		case "tool_use":
			toolName := ev.Name
			if toolName == "" {
				toolName = "tool"
			}
			progress.ReportTool(toolName)

		case "assistant":
			content := ev.Text
			if content == "" {
				content = ev.Content
			}
			assistantText.WriteString(content)

		case "result":
			content := ev.Text
			if content == "" {
				content = ev.Content
			}
			if content != "" {
				assistantText.Reset()
				assistantText.WriteString(content)
			}

			if ev.CostUSD > 0 || ev.NumTurns > 0 {
				if bc.tracker.RecordUsage(chat.ID, ev.NumTurns, ev.CostUSD, bc.config.MaxSessionTokens, ev.InputTokens, ev.OutputTokens) {
					log.Printf("session auto-reset: chat=%d threshold=%d", chat.ID, bc.config.MaxSessionTokens)
					// Flush nudge buffer before clearing so conversation memories are saved.
					if bc.dreamer != nil {
						cwd := bc.sessions.GetCwd(chat.ID, threadID)
						bc.dreamer.FlushNudge(chat.ID, threadID, cwd, bc.nudgeBuffer)
					}
					bc.sessions.Clear(chat.ID, threadID)
					bc.tracker.Clear(chat.ID)
				} else {
					usage := bc.tracker.Get(chat.ID)
					log.Printf("session usage: chat=%d %s", chat.ID, usage)
				}
			}

			finalText := strings.TrimSpace(assistantText.String())
			if finalText == "" {
				finalText = "(sem resposta)"
			}

			// Check if Aurelia emitted an execution plan
			if bc.orchestrator != nil {
				if plan, err := bc.orchestrator.ExtractPlan(finalText); err == nil && plan != nil {
					log.Printf("Execution plan detected with %d tasks", len(plan.Tasks))
					// Strip the plan block from the displayed text
					displayText := orchestrator.StripPlanBlock(finalText)
					if displayText != "" {
						_ = SendTextReplyWithThread(bc.bot, chat, displayText, messageID, threadID)
					}
					// Launch execution in background
					go bc.executeApprovedPlan(chat, messageID, plan)
					return outcomeSuccess
				}
			}

			if err := SendTextReplyWithThread(bc.bot, chat, finalText, messageID, threadID); err != nil {
				log.Printf("Failed to send reply to chat %d: %v", chat.ID, err)
			}
			if bc.dreamer != nil {
				bc.dreamer.AfterTurn()
				cwd := bc.sessions.GetCwd(chat.ID, threadID)
				bc.nudgeBuffer.AddTurn(chat.ID, userText, finalText)
				bc.dreamer.AfterTurnNudge(chat.ID, threadID, cwd, bc.nudgeBuffer)
			}
			return outcomeSuccess

		case "error":
			errMsg := ev.Message
			if errMsg == "" {
				errMsg = ev.Content
			}
			if errMsg == "" {
				errMsg = "Erro desconhecido no processador."
			}
			log.Printf("Bridge error: %s", errMsg)
			if err := SendErrorWithThread(bc.bot, chat, errMsg, threadID); err != nil {
				log.Printf("Failed to send error to chat %d: %v", chat.ID, err)
			}
			return outcomeLLMError

		default:
			log.Printf("Bridge event (ignored): %s", ev.Type)
		}
	}

	// Channel closed without terminal event — process died
	return outcomeProcessDeath
}

// buildSystemPrompt assembles the system prompt from persona, agent, cron/telegram instructions, and memory.
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
	telegramSection := bc.buildTelegramInstructions(chatID, messageID, threadID)
	telegramLen = len(telegramSection)
	sections = append(sections, telegramSection)

	// Auto-memory instructions (SDK auto-memory doesn't activate via programmatic API,
	// so we instruct the model explicitly)
	memorySection := bc.buildMemoryInstructions(chatID, threadID)
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

// detectProjectPath tries to find a project path from the user message.
// 1. Looks for absolute paths in the text
// 2. Searches memory files for project names mentioned in the text
// 3. Scans the user's home directory for directories matching words in the text
func (bc *BotController) detectProjectPath(text string) string {
	// 1. Absolute path in text (must be deep enough to be a real project)
	for _, word := range strings.Fields(text) {
		if !filepath.IsAbs(word) {
			continue
		}
		clean := filepath.Clean(word)
		// Reject trivial paths like "/" or "/home"
		if len(strings.Split(clean, string(filepath.Separator))) < 4 {
			continue
		}
		info, err := os.Stat(clean)
		if err == nil && info.IsDir() {
			return clean
		}
	}

	// 2. Match project names from memory files
	if bc.memoryDir != "" {
		if found := bc.detectFromMemoryFiles(text); found != "" {
			return found
		}
	}

	// 3. Scan disk for directory name match
	if found := scanForProject(text); found != "" {
		return found
	}

	return ""
}

// detectFromMemoryFiles searches memory files for projects mentioned in text.
func (bc *BotController) detectFromMemoryFiles(text string) string {
	entries, err := os.ReadDir(bc.memoryDir)
	if err != nil {
		return ""
	}

	lower := strings.ToLower(text)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "MEMORY.md" || !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(bc.memoryDir, name))
		if err != nil {
			continue
		}
		content := string(data)

		// Extract project name from frontmatter
		projectName := extractFrontmatterField(content, "name")
		if projectName == "" {
			continue
		}

		// Check if user message mentions this project (either direction match)
		lowerName := strings.ToLower(projectName)
		if !strings.Contains(lower, lowerName) && !strings.Contains(lowerName, lower) {
			// Try partial match — any word in the message that's a substring of the project name
			found := false
			for _, word := range strings.Fields(lower) {
				clean := strings.Trim(word, ".,!?;:()\"'")
				if len(clean) >= 4 && strings.Contains(lowerName, clean) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Extract path from content (look for "Caminho:" or absolute path lines)
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Caminho:") || strings.HasPrefix(line, "Path:") {
				path := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
				if info, err := os.Stat(path); err == nil && info.IsDir() {
					return path
				}
			}
		}
	}
	return ""
}

// skipDirs contains directory names to skip during disk scan.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, ".cache": true, ".local": true,
	".npm": true, ".cargo": true, ".rustup": true, "vendor": true,
	".vscode": true, ".idea": true, "__pycache__": true, ".tox": true,
	"dist": true, "build": true, ".next": true, ".nuxt": true,
	".gradle": true, ".m2": true, "target": true, ".docker": true,
	".virtualenvs": true, ".pyenv": true, ".nvm": true, ".sdkman": true,
}

// scanForProject walks the user's home directory (and mounted volumes) looking
// for a directory whose name fuzzy-matches a word from the user's message.
// Depth is limited and heavy directories are skipped for performance.
func scanForProject(text string) string {
	// Extract candidate words that look like project names.
	// A candidate must contain a hyphen, underscore, or digit — plain words are too ambiguous.
	var candidates []string
	for _, word := range strings.Fields(strings.ToLower(text)) {
		clean := strings.Trim(word, ".,!?;:()\"'/")
		if len(clean) < 3 || isStopWord(clean) {
			continue
		}
		if looksLikeProjectName(clean) {
			candidates = append(candidates, clean)
		}
	}
	if len(candidates) == 0 {
		return ""
	}

	// Roots to scan: home + mounted media volumes
	var roots []string
	home, _ := os.UserHomeDir()
	if home != "" {
		roots = append(roots, home)
	}
	// Common mount points for external drives
	for _, media := range []string{"/media", "/mnt"} {
		if userDirs, err := os.ReadDir(media); err == nil {
			for _, u := range userDirs {
				if u.IsDir() {
					roots = append(roots, filepath.Join(media, u.Name()))
				}
			}
		}
	}

	const maxDepth = 4

	for _, root := range roots {
		rootDepth := strings.Count(root, string(filepath.Separator))
		var result string

		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				if err != nil && d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			name := d.Name()

			// Skip hidden dirs (except the root itself) and known heavy dirs
			if path != root {
				if strings.HasPrefix(name, ".") || skipDirs[name] {
					return filepath.SkipDir
				}
			}

			// Depth limit
			depth := strings.Count(path, string(filepath.Separator)) - rootDepth
			if depth > maxDepth {
				return filepath.SkipDir
			}

			// Skip root dirs themselves and depth-1 dirs (too shallow to be projects)
			if depth < 2 {
				return nil
			}

			// Skip the home directory itself
			if home != "" && path == home {
				return nil
			}

			// Check if directory name matches any candidate
			lowerName := strings.ToLower(name)
			for _, c := range candidates {
				if lowerName == c || strings.Contains(lowerName, c) || strings.Contains(c, lowerName) {
					// Verify it looks like a project (has at least some files)
					entries, readErr := os.ReadDir(path)
					if readErr != nil || len(entries) < 2 {
						continue
					}
					result = path
					return filepath.SkipAll
				}
			}
			return nil
		})

		if result != "" {
			return result
		}
	}

	return ""
}

// isStopWord returns true for common words that shouldn't match project names.
var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true,
	"that": true, "this": true, "into": true, "have": true, "been": true,
	"will": true, "can": true, "are": true, "was": true, "were": true,
	"uma": true, "que": true, "com": true, "para": true,
	"por": true, "como": true, "mas": true, "mais": true, "esse": true,
	"essa": true, "isto": true, "isso": true, "aqui": true, "ali": true,
	"nos": true, "vou": true, "vamos": true, "dar": true, "olhada": true,
	"olhar": true, "ver": true, "projeto": true, "project": true,
	"look": true, "let": true, "check": true, "open": true,
}

func isStopWord(w string) bool {
	return stopWords[w]
}

// looksLikeProjectName returns true if the word looks like a project/repo name
// rather than a plain natural-language word. Indicators: hyphens, underscores,
// digits, dots, or camelCase transitions.
func looksLikeProjectName(w string) bool {
	if strings.ContainsAny(w, "-_.0123456789") {
		return true
	}
	// camelCase: lowercase followed by uppercase (e.g. "myProject")
	for i := 1; i < len(w); i++ {
		if w[i-1] >= 'a' && w[i-1] <= 'z' && w[i] >= 'A' && w[i] <= 'Z' {
			return true
		}
	}
	return false
}

// planningKeywords triggers orchestrator injection when present in user text.
var planningKeywords = []string{
	// Portuguese
	"implementa", "implemente", "implanta", "crie", "criar", "construa", "construir",
	"planejar", "planeje", "planeja", "plano", "spec", "design", "tarefa",
	"refatorar", "refatore", "migrar", "migre", "reescrever", "reescreva",
	"adicionar", "adicione", "feature", "funcionalidade",
	"aprovado", "pode fazer", "manda ver", "bora", "execute",
	// English
	"implement", "build", "create", "plan", "refactor", "migrate", "rewrite",
	"add feature", "approved", "execute", "ship it",
}

// looksLikePlanningIntent returns true when the user message suggests they want
// to plan, implement, or execute something — not just chat or ask questions.
func looksLikePlanningIntent(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range planningKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// extractFrontmatterField extracts a field value from YAML frontmatter.
func extractFrontmatterField(content string, field string) string {
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			if inFrontmatter {
				return "" // end of frontmatter, field not found
			}
			inFrontmatter = true
			continue
		}
		if inFrontmatter && strings.HasPrefix(trimmed, field+":") {
			return strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[1])
		}
	}
	return ""
}

// buildTelegramInstructions returns instructions for interacting with the Telegram chat.
func (bc *BotController) buildTelegramInstructions(chatID int64, messageID int, threadID int) string {
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
			if cwd := bc.sessions.GetCwd(chatID, threadID); cwd != "" {
				return cwd
			}
			return "(none — no project set)"
		}())
}

// buildMemoryInstructions returns the system prompt section for persistent memory.
// It reads memory files from up to 3 layers (global + project-private + project-team)
// and injects their contents so the model always has context — even after SDK context compaction.
func (bc *BotController) buildMemoryInstructions(chatID int64, threadID int) string {
	var sb strings.Builder

	cwd := bc.sessions.GetCwd(chatID, threadID)
	hasProject := cwd != "" && bc.resolver != nil

	topicSuffix := ""
	if threadID > 0 {
		topicSuffix = fmt.Sprintf(" (topic %d)", threadID)
	}

	sb.WriteString(fmt.Sprintf(`## Persistent Memory — YOU HAVE MEMORY

IMPORTANT: Unlike standard coding agents, you DO have persistent memory across conversations. Your memory contents are loaded below. NEVER say you "don't have memory" or that "each session starts from zero" — that is FALSE. Always check your memory contents below before answering questions about past conversations.`))

	// Saving instructions depend on whether project context is active
	if hasProject {
		projectDir := bc.resolver.ProjectMemoryDir(cwd)
		teamDir := bc.resolver.ProjectTeamMemoryDir(cwd)
		projectName := filepath.Base(cwd)
		topicMemoryDir := ""
		if threadID > 0 {
			topicMemoryDir = filepath.Join(bc.memoryDir, "topics", fmt.Sprint(threadID))
		}

		sb.WriteString("\n\n### Memory Layers" + topicSuffix + "\n\n")
		sb.WriteString("Save each fact in the correct layer:\n\n")
		sb.WriteString("| Layer | Directory | What to save |\n")
		sb.WriteString("|---|---|---|\n")
		sb.WriteString("| **Global** | " + bc.memoryDir + " | Personal facts, preferences, communication style — applies across all projects |\n")
		sb.WriteString("| **Project Private** | " + projectDir + " | Your personal notes, work log, individual decisions for project \"" + projectName + "\" |\n")
		sb.WriteString("| **Project Team** | " + teamDir + " | Stack, conventions, architecture, known bugs — useful for any team member on \"" + projectName + "\" |\n")
		if topicMemoryDir != "" {
			sb.WriteString("| **Topic** | " + topicMemoryDir + " | Facts specific to this forum topic — isolated from other topics |\n")
		}

		sb.WriteString("\n### Saving memory\n")
		sb.WriteString("When something meaningful happens, save it using the Write tool to the correct layer:\n")
		sb.WriteString("1. Write/update a topic file in the appropriate directory\n")
		sb.WriteString("2. Update the MEMORY.md index in that directory: one line per file as - [Title](file.md) — summary\n\n")
		sb.WriteString("Do NOT just promise to save — actually call Write before your response ends.")
	} else {
		topicMemoryDir := ""
		if threadID > 0 {
			topicMemoryDir = filepath.Join(bc.memoryDir, "topics", fmt.Sprint(threadID))
		}

		dirList := bc.memoryDir
		if topicMemoryDir != "" {
			dirList += " (global) and " + topicMemoryDir + " (this topic)"
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
	memoryContent := bc.loadMemoryContents(chatID, threadID)
	if memoryContent != "" {
		sb.WriteString("\n\n### Current Memory Contents"+topicSuffix+"\n\n")
		sb.WriteString(memoryContent)
	}

	return sb.String()
}

// loadMemoryContents reads memory files from all available layers and returns
// their contents for injection into the system prompt.
func (bc *BotController) loadMemoryContents(chatID int64, threadID int) string {
	var sb strings.Builder

	// Layer 1: Global memory (always)
	globalContent := loadMemoryDir(bc.memoryDir)
	if globalContent != "" {
		sb.WriteString("#### Global (cross-project)\n\n")
		sb.WriteString(globalContent)
	}

	// Layer 2: Topic memory (only when threadID > 0, i.e. forum topics)
	if threadID > 0 {
		topicDir := filepath.Join(bc.memoryDir, "topics", fmt.Sprint(threadID))
		topicContent := loadMemoryDir(topicDir)
		if topicContent != "" {
			fmt.Fprintf(&sb, "\n\n#### Topic %d\n\n", threadID)
			sb.WriteString(topicContent)
		}
	}

	// Layers 3 & 4: Project memory (only when cwd is set)
	cwd := bc.sessions.GetCwd(chatID, threadID)
	if cwd != "" && bc.resolver != nil {
		projectName := filepath.Base(cwd)

		projectDir := bc.resolver.ProjectMemoryDir(cwd)
		projectContent := loadMemoryDir(projectDir)
		if projectContent != "" {
			fmt.Fprintf(&sb, "\n\n#### Project: %s (private)\n\n", projectName)
			sb.WriteString(projectContent)
		}

		teamDir := bc.resolver.ProjectTeamMemoryDir(cwd)
		teamContent := loadMemoryDir(teamDir)
		if teamContent != "" {
			fmt.Fprintf(&sb, "\n\n#### Project: %s (team)\n\n", projectName)
			sb.WriteString(teamContent)
		}
	}

	return sb.String()
}

// loadMemoryDir reads MEMORY.md and all .md files from a directory.
// Memory files are loaded even when MEMORY.md is missing or empty so that
// memories saved by the model are never silently dropped.
func loadMemoryDir(dir string) string {
	if dir == "" {
		return ""
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var sb strings.Builder

	// Include the index if it has content.
	indexPath := filepath.Join(dir, "MEMORY.md")
	if indexData, err := os.ReadFile(indexPath); err == nil && len(indexData) > 0 {
		sb.WriteString("**MEMORY.md (index):**\n")
		sb.WriteString(string(indexData))
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "MEMORY.md" || !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(data) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\n\n**%s:**\n%s", name, strings.TrimSpace(string(data)))
	}

	return sb.String()
}

// buildProjectDocsSection reads CLAUDE.md and AGENTS.md from the active cwd
// (chat-level or agent-level) and injects them into the system prompt.
func (bc *BotController) buildProjectDocsSection(chatID int64, agent *agents.Agent, threadID int) string {
	// Resolve cwd: agent overrides chat-level
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

	// Read CLAUDE.md
	claudeMd, err := os.ReadFile(filepath.Join(cwd, "CLAUDE.md"))
	if err == nil && len(claudeMd) > 0 {
		parts = append(parts, fmt.Sprintf("# Project Instructions (CLAUDE.md)\n\n%s", strings.TrimSpace(string(claudeMd))))
	}

	// Read AGENTS.md
	agentsMd, err := os.ReadFile(filepath.Join(cwd, "AGENTS.md"))
	if err == nil && len(agentsMd) > 0 {
		parts = append(parts, fmt.Sprintf("# Squad Configuration (AGENTS.md)\n\n%s", strings.TrimSpace(string(agentsMd))))
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// buildCronInstructions returns the system prompt section that teaches the agent
// how to create and manage cron jobs via the aurelia CLI.
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
