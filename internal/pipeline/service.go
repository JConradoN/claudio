package pipeline

import (
	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/persona"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/session"
)

// ProgressReporter reports bridge tool activity to the chat transport.
type ProgressReporter interface {
	ReportTool(toolName string)
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
	ExecuteApprovedPlan(chatID int64, messageID int, plan *orchestrator.Plan)
}

// Dreamer receives turn lifecycle notifications for memory/nudge updates.
type Dreamer interface {
	AfterTurn()
	AfterTurnNudge(chatID int64, threadID int, cwd string, buffer *session.NudgeBuffer)
	FlushNudge(chatID int64, threadID int, cwd string, buffer *session.NudgeBuffer)
}

// Config contains dependencies needed by the business pipeline.
type Config struct {
	AppConfig    *config.AppConfig
	Bridge       *bridge.Bridge
	Agents       *agents.Registry
	Persona      *persona.CanonicalIdentityService
	Sessions     *session.Store
	Tracker      *session.Tracker
	Resolver     *runtime.PathResolver
	MemoryDir    string
	ExePath      string
	BotCwd       string
	Output       Output
	Orchestrator *orchestrator.Orchestrator
	Dreamer      Dreamer
	ProjectIndex *runtime.ProjectIndex
}

// Service owns the LLM/message pipeline independent from Telegram routing.
type Service struct {
	config         *config.AppConfig
	bridge         *bridge.Bridge
	agents         *agents.Registry
	persona        *persona.CanonicalIdentityService
	sessions       *session.Store
	tracker        *session.Tracker
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
	bridgeFailures FailureTracker
	runs           *runSupervisor
}

// NewService builds a pipeline service with explicit dependencies.
func NewService(cfg Config) *Service {
	return &Service{
		config:       cfg.AppConfig,
		bridge:       cfg.Bridge,
		agents:       cfg.Agents,
		persona:      cfg.Persona,
		sessions:     cfg.Sessions,
		tracker:      cfg.Tracker,
		resolver:     cfg.Resolver,
		memoryDir:    cfg.MemoryDir,
		exePath:      cfg.ExePath,
		botCwd:       cfg.BotCwd,
		output:       cfg.Output,
		orchestrator: cfg.Orchestrator,
		dreamer:      cfg.Dreamer,
		nudgeBuffer:  session.NewNudgeBuffer(),
		memoryCache:  newMemoryCache(),
		projectIndex: cfg.ProjectIndex,
		runs:         newRunSupervisor(),
	}
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

// NudgeBuffer returns the per-service nudge buffer for command-triggered flushes.
func (s *Service) NudgeBuffer() *session.NudgeBuffer {
	return s.nudgeBuffer
}
