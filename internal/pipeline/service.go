package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/persona"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/security"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/internal/users"
)

// ProgressReporter reports bridge tool activity to the chat transport.
type ProgressReporter interface {
	ReportTool(toolName string)
	ReportToolResult(summary string)
	ReportText(text string)
	Delete()
}

// Output adapts pipeline responses to a chat transport such as Telegram.
type Output interface {
	StartTyping(chatID int64, threadID int) func()
	NewProgress(chatID int64, threadID int) ProgressReporter
	SendError(chatID int64, threadID int, text string) error
	SendReply(chatID int64, threadID int, text string) error
	SendText(chatID int64, threadID int, text string) (any, error)
	DeleteMessage(message any)
	ConfirmMessage(chatID int64, messageID int)
	ExecuteApprovedPlan(chatID int64, messageID int, plan *orchestrator.Plan)
}

// Dreamer receives turn lifecycle notifications for memory/nudge updates.
type Dreamer interface {
	AfterTurn(userID int64)
	AfterTurnNudge(chatID int64, threadID int, userID int64, cwd string, buffer *session.NudgeBuffer)
	FlushNudge(chatID int64, threadID int, userID int64, cwd string, buffer *session.NudgeBuffer)
}

// Config contains dependencies needed by the business pipeline.
type Config struct {
	AppConfig    *config.AppConfig
	Bridge       *bridge.Bridge
	Agents       *agents.Registry
	Persona      *persona.CanonicalIdentityService
	Sessions     *session.Store
	Resolver     *runtime.PathResolver
	MemoryDir    string
	ExePath      string
	BotCwd       string
	Output       Output
	Orchestrator *orchestrator.Orchestrator
	Dreamer      Dreamer
	ProjectIndex *runtime.ProjectIndex
	Bindings     projectbinding.Store
	RunLog       runlog.Store
	Continuity   continuity.Store
	UsersStore   *users.Store
	UserResolver *users.Resolver
}

// Service owns the LLM/message pipeline independent from Telegram routing.
type Service struct {
	config         *config.AppConfig
	bridge         *bridge.Bridge
	resilient      *ResilientBridge
	agents         *agents.Registry
	persona        *persona.CanonicalIdentityService
	sessions       *session.Store
	resolver       *runtime.PathResolver
	memoryDir      string
	exePath        string
	botCwd         string
	output         Output
	orchestrator   *orchestrator.Orchestrator
	dreamer        Dreamer
	nudgeBuffer    *session.NudgeBuffer
	memoryCache    *memoryCache
	projectIndex   *runtime.ProjectIndex
	bindings       projectbinding.Store
	bridgeFailures FailureTracker
	activeSessions sync.Map // "chatID:threadID" → context.CancelFunc
	runLog         runlog.Store
	runLogMu       sync.Mutex
	runLogStates   map[string]*runLogState
	continuity     continuity.Store
	summaryCounter *summaryCounter
	summaryInterval int
	usersStore     *users.Store
	userResolver   *users.Resolver
}

const defaultSummaryInterval = 5

// NewService builds a pipeline service with explicit dependencies.
func NewService(cfg Config) *Service {
	s := &Service{
		config:       cfg.AppConfig,
		bridge:       cfg.Bridge,
		agents:       cfg.Agents,
		persona:      cfg.Persona,
		sessions:     cfg.Sessions,
		resolver:     cfg.Resolver,
		memoryDir:    cfg.MemoryDir,
		exePath:      cfg.ExePath,
		botCwd:       cfg.BotCwd,
		output:       cfg.Output,
		orchestrator: cfg.Orchestrator,
		dreamer:      cfg.Dreamer,
		nudgeBuffer:  session.NewNudgeBuffer(),
		memoryCache:  newMemoryCache(),
		projectIndex:  cfg.ProjectIndex,
		bindings:      cfg.Bindings,
		runLog:          cfg.RunLog,
		runLogStates:    make(map[string]*runLogState),
		continuity:      cfg.Continuity,
		summaryCounter:  &summaryCounter{counts: make(map[continuity.ConversationKey]int)},
		summaryInterval: defaultSummaryInterval,
		usersStore:      cfg.UsersStore,
		userResolver:    cfg.UserResolver,
	}

	if cfg.Bridge != nil {
		rbCfg := DefaultResilientConfig()
		if cfg.AppConfig != nil {
			rbCfg.OpenRouterAPIKey = cfg.AppConfig.ProviderAPIKey("openrouter")
		}
		s.resilient = NewResilientBridge(cfg.Bridge, rbCfg)
		s.resilient.ContinuitySnapshot = s.continuitySnapshot
	}

	if s.config != nil && s.config.SummaryInterval > 0 {
		s.summaryInterval = s.config.SummaryInterval
	}

	return s
}

// Cancel stops the active run for a chat thread by sending abort to bridge.
func (s *Service) Cancel(chatID int64, threadID int, userID ...int64) bool {
	if s == nil {
		return false
	}
	key := sessionKey(chatID, threadID)

	// Stop the old goroutine so it doesn't retry after abort
	if cancelVal, loaded := s.activeSessions.LoadAndDelete(key); loaded {
		if cancel, ok := cancelVal.(context.CancelFunc); ok {
			cancel()
		}
	} else {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
	defer cancel()
	_, err := s.bridge.ExecuteSync(ctx, bridge.Request{
		Command: "abort",
		Options: bridge.RequestOptions{ChatID: chatID, ThreadID: threadID},
	})
	return err == nil
}

// CancelAllForUser cancels all active sessions for a given user.
// Iterates the local activeSessions map and sends abort for each matching session.
func (s *Service) CancelAllForUser(userID int64) bool {
	if s == nil {
		return false
	}
	cancelled := false
	s.activeSessions.Range(func(key, value interface{}) bool {
		// key is "chatID:threadID" — we can't extract userID from it
		// For now, send abort-all command to bridge with userID
		// The bridge will track userID per session in a future iteration
		// For MVP, we cancel all sessions on the bridge side
		return true
	})
	// Broadcast abort to bridge — it will cancel all sessions
	ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
	defer cancel()
	_, err := s.bridge.ExecuteSync(ctx, bridge.Request{
		Command: "abort",
	})
	if err == nil {
		cancelled = true
	}
	return cancelled
}

// WorkStatus returns the active session status from the bridge.
// Returns a description string and the pending message count.
func (s *Service) WorkStatus(chatID int64, threadID int, userID ...int64) (string, int) {
	if s == nil {
		return "", 0
	}
	key := sessionKey(chatID, threadID)
	if _, active := s.activeSessions.Load(key); !active {
		return "", 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
	defer cancel()
	ev, err := s.bridge.ExecuteSync(ctx, bridge.Request{
		Command: "get-state",
		Options: bridge.RequestOptions{ChatID: chatID, ThreadID: threadID},
	})
	if err != nil {
		return "", 0
	}

	var state struct {
		IsStreaming  bool `json:"is_streaming"`
		PendingCount int  `json:"pending_count"`
	}
	if err := json.Unmarshal([]byte(ev.Content), &state); err != nil {
		return "", 0
	}

	desc := "rodando"
	if !state.IsStreaming {
		desc = "processando"
	}
	return desc, state.PendingCount
}

// SetOrchestrator injects the orchestrator after construction.
func (s *Service) SetOrchestrator(o *orchestrator.Orchestrator) {
	s.orchestrator = o
}

// SetProjectIndex injects a cached project name index for fast lookup.
func (s *Service) SetProjectIndex(pi *runtime.ProjectIndex) {
	s.projectIndex = pi
}

// SetDreamer injects the dream system after construction.
func (s *Service) SetDreamer(d Dreamer) {
	s.dreamer = d
}

// SetRunLog injects the run log store after construction (optional).
func (s *Service) SetRunLog(rl runlog.Store) {
	s.runLog = rl
}

// SetContinuity injects the continuity store after construction (optional).
func (s *Service) SetContinuity(cs continuity.Store) {
	s.continuity = cs
}

// NudgeBuffer returns the per-service nudge buffer for command-triggered flushes.
func (s *Service) NudgeBuffer() *session.NudgeBuffer {
	return s.nudgeBuffer
}

// getSecurityConfig returns the security configuration from AppConfig,
// falling back to safe defaults if not configured.
func (s *Service) getSecurityConfig() security.SecurityConfig {
	if s.config != nil {
		return s.config.SecurityConfig
	}
	return security.DefaultConfig()
}

// sessionKey builds a string key for the activeSessions map.
func sessionKey(chatID int64, threadID int) string {
	return fmt.Sprintf("%d:%d", chatID, threadID)
}

const bridgeCommandTimeout = 10 * time.Second
