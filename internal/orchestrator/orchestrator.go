package orchestrator

import (
	"context"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

// BridgeExecutor is the interface the orchestrator needs from the bridge.
type BridgeExecutor interface {
	Execute(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error)
	ExecuteSync(ctx context.Context, req bridge.Request) (*bridge.Event, error)
}

// OrchestratorConfig holds configuration for the orchestrator.
type OrchestratorConfig struct {
	MaxConcurrentWorkers int    // default: 3
	DefaultMaxTurns      int    // default: 25
	RepoRoot             string // project root path
}

// Orchestrator coordinates the plan→execute→validate→consolidate cycle.
type Orchestrator struct {
	bridge   BridgeExecutor
	worktree *WorktreeManager
	config   OrchestratorConfig
}

// NewOrchestrator creates a new Orchestrator with the given dependencies.
func NewOrchestrator(bridge BridgeExecutor, config OrchestratorConfig) *Orchestrator {
	if config.MaxConcurrentWorkers <= 0 {
		config.MaxConcurrentWorkers = 3
	}
	if config.DefaultMaxTurns <= 0 {
		config.DefaultMaxTurns = 25
	}

	var wm *WorktreeManager
	if config.RepoRoot != "" {
		wm = NewWorktreeManager(config.RepoRoot)
	}

	return &Orchestrator{
		bridge:   bridge,
		worktree: wm,
		config:   config,
	}
}

// Config returns the orchestrator configuration.
func (o *Orchestrator) Config() OrchestratorConfig {
	return o.config
}

// Consolidate calls Aurelia to synthesize the final results from all workers.
func (o *Orchestrator) Consolidate(ctx context.Context, plan *Plan, results []TaskResult, systemPrompt string) (string, error) {
	req := bridge.Request{
		Command: "query",
		Prompt:  systemPrompt,
		Options: bridge.RequestOptions{
			NoUserSettings: true,
		},
	}

	ev, err := o.bridge.ExecuteSync(ctx, req)
	if err != nil {
		return "", err
	}

	content := ev.Content
	if content == "" {
		content = ev.Text
	}
	return content, nil
}
