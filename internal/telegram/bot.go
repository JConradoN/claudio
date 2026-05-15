package telegram

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/cron"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/persona"
	pipelinepkg "github.com/igormaneschy/aurelia/internal/pipeline"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/internal/version"
	"github.com/igormaneschy/aurelia/pkg/stt"
)

// BotController wires Telegram I/O to the application services.
type BotController struct {
	bot              *telebot.Bot
	config           *config.AppConfig
	bridge           *bridge.Bridge
	agents           *agents.Registry
	persona          *persona.CanonicalIdentityService
	stt              stt.Transcriber
	cronHandler      *CronCommandHandler
	sessions         *session.Store
	tracker          *session.Tracker
	resolver         *runtime.PathResolver
	personasDir      string
	memoryDir        string // path to ~/.aurelia/memory for SDK auto-memory
	exePath          string // path to aurelia binary for CLI instructions in system prompt
	bootstrapMu      sync.Mutex
	pendingBootstrap map[int64]bootstrapState
	albums           *albumBuffer
	bridgeFailures   bridgeFailureTracker
	orchestrator     *orchestrator.Orchestrator
	nudgeBuffer      *session.NudgeBuffer
	botCwd           string // working directory of the aurelia daemon
	dreamer          interface {
		AfterTurn()
		AfterTurnNudge(chatID int64, threadID int, cwd string, buffer *session.NudgeBuffer)
		FlushNudge(chatID int64, threadID int, cwd string, buffer *session.NudgeBuffer)
	}
	modelCache         []bridge.ModelInfo
	modelCacheMu       sync.Mutex
	modelCacheExpiry   time.Time
	refreshProviderEnv func() // optional hook to re-export provider env vars after /model
	allowedUsers       map[int64]struct{}
	allowedGroups      map[int64]struct{}
	projectIndex       *runtime.ProjectIndex
	pipeline           *pipelinepkg.Service
}

type albumBuffer struct {
	mu      sync.Mutex
	pending map[string]*pendingAlbum
}

func newAlbumBuffer() *albumBuffer {
	return &albumBuffer{
		pending: make(map[string]*pendingAlbum),
	}
}

type pendingAlbum struct {
	ownerMessageID int
	caption        string
	photos         []albumPhoto
	chatID         int64
	threadID       int
	senderID       int64
	firstMessageID int
}

type albumPhoto struct {
	messageID int
	photo     telebot.Photo
}

// NewBotController builds the Telegram controller.
func NewBotController(
	cfg *config.AppConfig,
	br *bridge.Bridge,
	ag *agents.Registry,
	p *persona.CanonicalIdentityService,
	s stt.Transcriber,
	cronHandler *CronCommandHandler,
	personasDir string,
	memoryDir string,
	exePath string,
	sessions *session.Store,
	tracker *session.Tracker,
	resolver *runtime.PathResolver,
) (*BotController, error) {

	pref := telebot.Settings{
		Token:  cfg.TelegramBotToken,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := telebot.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	botCwd, _ := os.Getwd()

	allowedUsers := make(map[int64]struct{}, len(cfg.TelegramAllowedUserIDs))
	for _, id := range cfg.TelegramAllowedUserIDs {
		allowedUsers[id] = struct{}{}
	}
	allowedGroups := make(map[int64]struct{}, len(cfg.TelegramAllowedGroupIDs))
	for _, id := range cfg.TelegramAllowedGroupIDs {
		allowedGroups[id] = struct{}{}
	}

	bc := &BotController{
		bot:              b,
		config:           cfg,
		bridge:           br,
		agents:           ag,
		persona:          p,
		stt:              s,
		cronHandler:      cronHandler,
		sessions:         sessions,
		tracker:          tracker,
		resolver:         resolver,
		nudgeBuffer:      session.NewNudgeBuffer(),
		personasDir:      personasDir,
		memoryDir:        memoryDir,
		exePath:          exePath,
		botCwd:           botCwd,
		pendingBootstrap: make(map[int64]bootstrapState),
		albums:           newAlbumBuffer(),
		allowedUsers:     allowedUsers,
		allowedGroups:    allowedGroups,
	}
	bc.pipeline = pipelinepkg.NewService(pipelinepkg.Config{
		AppConfig: bc.config,
		Bridge:    bc.bridge,
		Agents:    bc.agents,
		Persona:   bc.persona,
		Sessions:  bc.sessions,
		Tracker:   bc.tracker,
		Resolver:  bc.resolver,
		MemoryDir: bc.memoryDir,
		ExePath:   bc.exePath,
		BotCwd:    bc.botCwd,
		Output:    telegramPipelineOutput{bc: bc},
	})
	bc.nudgeBuffer = bc.pipeline.NudgeBuffer()

	bc.setupRoutes()
	return bc, nil
}

// SetOrchestrator injects the orchestrator after construction.
// Called separately to avoid changing the NewBotController signature.
func (bc *BotController) SetOrchestrator(o *orchestrator.Orchestrator) {
	bc.orchestrator = o
	bc.ensurePipeline().SetOrchestrator(o)
}

// SetProviderEnvRefresher installs a callback that will be invoked after the
// user changes the default model via /model. The callback is expected to
// re-export the API key env vars for the new provider so the bridge picks
// them up on the next query.
func (bc *BotController) SetProviderEnvRefresher(f func()) {
	bc.refreshProviderEnv = f
}

// SetProjectIndex injects a cached project name index for fast lookup.
func (bc *BotController) SetProjectIndex(pi *runtime.ProjectIndex) {
	bc.projectIndex = pi
	bc.ensurePipeline().SetProjectIndex(pi)
}

// SetDreamer injects the dream system after construction.
func (bc *BotController) SetDreamer(d interface {
	AfterTurn()
	AfterTurnNudge(chatID int64, threadID int, cwd string, buffer *session.NudgeBuffer)
	FlushNudge(chatID int64, threadID int, cwd string, buffer *session.NudgeBuffer)
}) {
	bc.dreamer = d
	bc.ensurePipeline().SetDreamer(d)
}

// ChatSender returns a cron.ChatSender backed by this bot instance.
func (bc *BotController) ChatSender() cron.ChatSender {
	return &botChatSender{bot: bc.bot}
}

// botChatSender adapts a telebot.Bot to the cron.ChatSender interface.
type botChatSender struct {
	bot *telebot.Bot
}

func (s *botChatSender) Send(chatID int64, text string) error {
	_, err := s.bot.Send(&telebot.Chat{ID: chatID}, text, &telebot.SendOptions{DisableWebPagePreview: true})
	return err
}

// getModels returns cached models or fetches from bridge with 5-minute TTL.
func (bc *BotController) getModels(ctx context.Context) ([]bridge.ModelInfo, error) {
	bc.modelCacheMu.Lock()
	if bc.modelCache != nil && time.Now().Before(bc.modelCacheExpiry) {
		cached := bc.modelCache
		bc.modelCacheMu.Unlock()
		return cached, nil
	}
	bc.modelCacheMu.Unlock()

	models, err := bc.bridge.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	bc.modelCacheMu.Lock()
	bc.modelCache = models
	bc.modelCacheExpiry = time.Now().Add(5 * time.Minute)
	bc.modelCacheMu.Unlock()
	return models, nil
}

// Start begins Telegram polling.
func (bc *BotController) Start() {
	log.Printf("Starting %s Telegram Bot...", version.BuildInfo())
	bc.bot.Start()
}

// Stop ends Telegram polling.
func (bc *BotController) Stop() {
	bc.bot.Stop()
}

func (bc *BotController) isAllowedUser(userID int64) bool {
	if bc == nil || bc.allowedUsers == nil {
		return false
	}
	_, ok := bc.allowedUsers[userID]
	return ok
}

func (bc *BotController) isAllowedGroup(chatID int64) bool {
	if bc == nil || bc.allowedGroups == nil {
		return false
	}
	_, ok := bc.allowedGroups[chatID]
	return ok
}

func (bc *BotController) setupRoutes() {
	bc.bot.Use(bc.whitelistMiddleware())

	bc.setupBootstrapRoutes()
	bc.registerContentRoutes()
	bc.registerSlashMenu()
}
