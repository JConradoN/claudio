package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/cron"
	"github.com/igormaneschy/aurelia/internal/deps"
	"github.com/igormaneschy/aurelia/internal/dream"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/persona"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/internal/telegram"
	"github.com/igormaneschy/aurelia/pkg/stt"
)

type app struct {
	resolver   *runtime.PathResolver
	bridge     *bridge.Bridge
	agents     *agents.Registry
	cronStore  *cron.SQLiteCronStore
	bot        *telegram.BotController
	scheduler  *cron.Scheduler
	cronCtx    context.Context
	cronCancel context.CancelFunc
}

var runClaudeAuthStatus = func() ([]byte, error) {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			fallback := filepath.Join(home, ".local", "bin", "claude")
			if stat, statErr := os.Stat(fallback); statErr == nil && !stat.IsDir() {
				claudePath = fallback
			}
		}
	}
	if claudePath == "" {
		return nil, err
	}
	return exec.Command(claudePath, "auth", "status").Output()
}

func bootstrapApp() (*app, error) {
	resolver, err := runtime.New()
	if err != nil {
		return nil, fmt.Errorf("resolve instance root: %w", err)
	}
	if err := runtime.Bootstrap(resolver); err != nil {
		return nil, fmt.Errorf("bootstrap instance directory: %w", err)
	}

	cfg, err := config.Load(resolver)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	setProviderEnv(cfg)

	// Check runtime dependencies before touching the bridge.
	checkResult := deps.CheckAll()
	for _, d := range checkResult.Deps {
		if d.Required && !d.Found {
			log.Fatalf("%s is required but not found. Install: %s", d.Name, d.InstallURL)
		}
		if d.Required && d.Found && !d.VersionOK {
			log.Fatalf("%s v%s found but >= %s required. Update: %s", d.Name, d.Version, d.MinVersion, d.InstallURL)
		}
		if !d.Required && !d.Found {
			log.Printf("Warning: %s not found — some features may be limited", d.Name)
		}
	}

	br := setupBridge()
	personaSvc := setupPersona(resolver)

	agentReg, err := agents.Load(resolver.Agents())
	if err != nil {
		log.Printf("Warning: failed to load agents registry: %v (continuing without agents)", err)
		agentReg = nil
	}

	cronStore, err := cron.NewSQLiteCronStore(resolver.DBPath("cron.db"))
	if err != nil {
		return nil, fmt.Errorf("initialize cron store: %w", err)
	}

	transcriber, err := buildTranscriber(cfg)
	if err != nil {
		if closeErr := cronStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close cron store: %v", closeErr)
		}
		return nil, fmt.Errorf("initialize transcriber: %w", err)
	}

	cronSvc := cron.NewService(cronStore, nil)
	cronHandler := telegram.NewCronCommandHandler(cronSvc)
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("Warning: failed to resolve executable path: %v", err)
	}
	sessions := session.NewStore()
	tracker := session.NewTracker()

	br.SetOnDeath(func() {
		log.Printf("bridge: process died, deactivating all sessions")
		sessions.DeactivateAll()
	})

	bot, err := telegram.NewBotController(
		cfg, br, agentReg, personaSvc, transcriber,
		cronHandler, resolver.MemoryPersonas(), resolver.Memory(), exePath, sessions, tracker, resolver,
	)
	if err != nil {
		if closeErr := cronStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close cron store: %v", closeErr)
		}
		return nil, fmt.Errorf("initialize telegram bot: %w", err)
	}

	// Wire orchestrator — enables autonomous agent orchestration
	cwd, _ := os.Getwd()
	orch := orchestrator.NewOrchestrator(br, orchestrator.OrchestratorConfig{
		RepoRoot: cwd,
	})
	bot.SetOrchestrator(orch)

	// When the user changes the default model via /model, the live cfg has been
	// mutated already; re-export the provider env vars so the bridge picks up
	// the right API key on its next query.
	bot.SetProviderEnvRefresher(func() { setProviderEnv(cfg) })

	// Wire dreamer — background memory consolidation + nudge review.
	// Fall back to the user's default model so dream/nudge work on any provider
	// (avoids 402 errors when the hardcoded Anthropic models aren't available).
	dreamCfg := dream.DefaultConfig()
	dreamCfg.Provider = cfg.DefaultProvider
	userModel := cfg.DefaultModel
	if userModel != "" {
		dreamCfg.Model = userModel
		dreamCfg.ExtractModel = userModel
		dreamCfg.NudgeModel = userModel
	}
	if cfg.DreamModel != "" {
		dreamCfg.Model = cfg.DreamModel
	}
	if cfg.ExtractModel != "" {
		dreamCfg.ExtractModel = cfg.ExtractModel
	}
	if cfg.NudgeEnabled != nil {
		dreamCfg.NudgeEnabled = *cfg.NudgeEnabled
	}
	if cfg.NudgeTurns > 0 {
		dreamCfg.NudgeTurns = cfg.NudgeTurns
	}
	if cfg.NudgeModel != "" {
		dreamCfg.NudgeModel = cfg.NudgeModel
	}
	dreamer := dream.New(resolver.Memory(), resolver, br, dreamCfg)
	bot.SetDreamer(dreamer)

	scheduler, err := setupCronScheduler(cronStore, br, agentReg, personaSvc, bot, resolver.Memory(), cfg.DefaultProvider)
	if err != nil {
		if closeErr := cronStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close cron store: %v", closeErr)
		}
		return nil, fmt.Errorf("initialize cron scheduler: %w", err)
	}

	cronCtx, cronCancel := context.WithCancel(context.Background())

	return &app{
		resolver:   resolver,
		bridge:     br,
		agents:     agentReg,
		cronStore:  cronStore,
		bot:        bot,
		scheduler:  scheduler,
		cronCtx:    cronCtx,
		cronCancel: cronCancel,
	}, nil
}

// setupBridge creates the Bridge, ensuring ~/.aurelia/bridge/ is bootstrapped.
func setupBridge() *bridge.Bridge {
	home, _ := os.UserHomeDir()
	aureliBridgeDir := filepath.Join(home, ".aurelia", "bridge")
	if _, setupErr := bridge.EnsureBridge(aureliBridgeDir, bridge.EmbeddedBundleJS); setupErr != nil {
		log.Printf("Warning: bridge auto-setup failed: %v", setupErr)
	}
	bridgeDir := findBridgeDir()
	if bridgeDir == "" {
		bridgeDir = aureliBridgeDir
	}
	bundlePath := filepath.Join(bridgeDir, "bundle.js")

	// minBundleSize guards against a truncated bundle.js (e.g. an aborted
	// esbuild run leaving an empty file). The real bundle is ~12 MB; below
	// this threshold we assume corruption and fall back to running tsx.
	const minBundleSize = 10 * 1024
	switch info, err := os.Stat(bundlePath); {
	case err != nil:
		// Missing bundle — let the bridge fall back to tsx.
		bundlePath = ""
	case info.Size() < minBundleSize:
		log.Printf("warning: bridge bundle.js exists but is only %d bytes (<%d); using tsx fallback", info.Size(), minBundleSize)
		bundlePath = ""
	}

	return bridge.New(bridgeDir, bundlePath)
}

// setupPersona builds the canonical identity service from persona and playbook files.
func setupPersona(resolver *runtime.PathResolver) *persona.CanonicalIdentityService {
	personasDir := resolver.MemoryPersonas()
	memoryDir := resolver.Memory()
	ownerPlaybookPath := filepath.Join(memoryDir, "OWNER_PLAYBOOK.md")
	lessonsLearnedPath := filepath.Join(memoryDir, "LESSONS_LEARNED.md")

	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("Warning: failed to resolve working directory for project playbook: %v", err)
		cwd = ""
	}
	if err := runtime.BootstrapProject(cwd); err != nil {
		log.Printf("Warning: failed to bootstrap project-local Aurelia directory: %v", err)
	}
	var projectPlaybookPath string
	if cwd != "" {
		projectPlaybookPath = filepath.Join(cwd, "docs", "PROJECT_PLAYBOOK.md")
	}

	return persona.NewCanonicalIdentityService(
		filepath.Join(personasDir, "IDENTITY.md"),
		filepath.Join(personasDir, "SOUL.md"),
		filepath.Join(personasDir, "USER.md"),
		ownerPlaybookPath,
		lessonsLearnedPath,
		projectPlaybookPath,
	)
}

// telegramChatSender adapts a telebot.Bot to the cron.ChatSender interface.
type telegramChatSender struct {
	bot *telebot.Bot
}

func (s *telegramChatSender) Send(chatID int64, text string) error {
	chat := &telebot.Chat{ID: chatID}
	return telegram.SendText(s.bot, chat, text)
}

// setupCronScheduler creates the cron scheduler with Telegram delivery.
// Returns nil scheduler if agentReg is nil.
func setupCronScheduler(
	cronStore *cron.SQLiteCronStore,
	br *bridge.Bridge,
	agentReg *agents.Registry,
	personaSvc *persona.CanonicalIdentityService,
	bot *telegram.BotController,
	memoryDir string,
	defaultProvider string,
) (*cron.Scheduler, error) {
	if agentReg == nil {
		return nil, nil
	}

	cronRuntime := cron.NewBridgeCronRuntime(
		&cron.BridgeAdapter{B: br},
		agentReg,
		personaSvc,
		memoryDir,
		defaultProvider,
	)

	delivery := cron.NewTelegramDelivery(&telegramChatSender{bot: bot.GetBot()})
	deliverFn := func(ctx context.Context, job cron.CronJob, result *cron.ExecutionResult, execErr error) error {
		return delivery.Deliver(ctx, job, result, execErr)
	}

	notifyingRuntime := cron.NewNotifyingRuntime(cronRuntime, deliverFn)
	scheduler, err := cron.NewScheduler(cronStore, notifyingRuntime, nil, cron.SchedulerConfig{
		PollInterval: 15 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	registerScheduledAgents(cronStore, agentReg)
	return scheduler, nil
}

func (a *app) start() {
	if a.scheduler != nil {
		go func() {
			if err := a.scheduler.Start(a.cronCtx); err != nil && err != context.Canceled {
				log.Printf("Warning: cron scheduler stopped with error: %v", err)
			}
		}()
	}
	go a.bot.Start()
}

func (a *app) shutdown(ctx context.Context) {
	if a.cronCancel != nil {
		a.cronCancel()
	}
	if a.bot != nil {
		done := make(chan struct{})
		go func() {
			a.bot.Stop()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			log.Println("Warning: bot shutdown timed out")
		}
	}
}

func (a *app) close() {
	if a.bridge != nil {
		a.bridge.Stop()
	}
	if a.cronStore != nil {
		if err := a.cronStore.Close(); err != nil {
			log.Printf("Warning: failed to close cron store: %v", err)
		}
	}
}

// setProviderEnv exports provider credentials as env vars consumed by PI.
func setProviderEnv(cfg *config.AppConfig) {
	provider := config.NormalizeProvider(cfg.DefaultProvider)
	authMode := cfg.ProviderAuthMode(provider)

	// Subscription mode remains supported for Anthropic when the PI auth store is
	// already configured. We also keep the legacy Claude auth check as fallback.
	if provider == "anthropic" && authMode == "subscription" {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("ANTHROPIC_BASE_URL")
		if hasPIAuth("anthropic") {
			return
		}
		home, _ := os.UserHomeDir()
		if !hasClaudeSubscriptionAuth(home) {
			log.Fatalf("Anthropic subscription requires PI /login or Claude auth. Run 'pi /login' or 'claude auth login' first.")
		}
		return
	}

	apiKey := cfg.ProviderAPIKey(provider)
	if apiKey == "" {
		return
	}

	if envName := piProviderEnvName(provider); envName != "" {
		_ = os.Setenv(envName, apiKey)
	}
}

func piProviderEnvName(provider string) string {
	switch config.NormalizeProvider(provider) {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "kimi":
		return "KIMI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	case "zai":
		return "ZAI_API_KEY"
	case "google":
		return "GEMINI_API_KEY"
	case "kilo":
		return "OPENCODE_API_KEY"
	default:
		return ""
	}
}

func hasPIAuth(provider string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	path := filepath.Join(home, ".pi", "agent", "auth.json")
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return false
	}
	var auth map[string]any
	if err := json.Unmarshal(data, &auth); err != nil {
		return false
	}
	_, ok := auth[provider]
	return ok
}

func hasClaudeSubscriptionAuth(home string) bool {
	if home != "" {
		credPath := filepath.Join(home, ".claude", ".credentials.json")
		if stat, err := os.Stat(credPath); err == nil && !stat.IsDir() && stat.Size() > 0 {
			return true
		}
	}

	out, err := runClaudeAuthStatus()
	if err != nil {
		return false
	}

	var status struct {
		LoggedIn   bool   `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return false
	}
	return status.LoggedIn && status.AuthMethod == "claude.ai"
}

// findBridgeDir returns ~/.aurelia/bridge/ as the canonical bridge directory.
func findBridgeDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aurelia", "bridge")
}

func buildTranscriber(cfg *config.AppConfig) (stt.Transcriber, error) {
	switch cfg.STTProvider {
	case "", "groq":
		return stt.NewGroqTranscriber(cfg.ProviderAPIKey("groq")), nil
	default:
		return nil, fmt.Errorf("unsupported stt provider %q", cfg.STTProvider)
	}
}

// registerScheduledAgents syncs agent schedules into the cron store.
// Uses a deterministic job ID derived from agent name so that restarts
// skip agents that already have a job registered (idempotent).
func registerScheduledAgents(store *cron.SQLiteCronStore, reg *agents.Registry) {
	if reg == nil {
		return
	}
	svc := cron.NewService(store, nil)
	for _, a := range reg.Scheduled() {
		jobID := "scheduled-agent-" + a.Name

		// Skip if a job with this ID already exists.
		existing, err := store.GetJob(context.Background(), jobID)
		if err != nil {
			log.Printf("Warning: failed to check existing job for agent %q: %v", a.Name, err)
			continue
		}
		if existing != nil {
			log.Printf("Scheduled agent %q already registered (job %s), skipping", a.Name, jobID)
			continue
		}

		_, err = svc.CreateJob(context.Background(), cron.CronJob{
			ID:           jobID,
			AgentName:    a.Name,
			ScheduleType: "cron",
			CronExpr:     a.Schedule,
			Prompt:       a.Prompt,
		})
		if err != nil {
			log.Printf("Warning: failed to register scheduled agent %q: %v", a.Name, err)
		}
	}
}
