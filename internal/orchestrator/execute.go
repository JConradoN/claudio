package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
)

// ExecuteTask runs a single task as a worker via the bridge.
// It streams events and calls onEvent for visual feedback.
func (o *Orchestrator) ExecuteTask(
	ctx context.Context,
	task Task,
	cfg WorkerConfig,
	cwd string,
	systemPrompt string,
	onEvent func(WorkerEvent),
) TaskResult {
	start := time.Now()

	onEvent(WorkerEvent{TaskID: task.ID, Type: "start", Message: task.Description})

	req := bridge.Request{
		Command: "query",
		Prompt:  task.Prompt,
		Options: bridge.RequestOptions{
			Model:          cfg.Model,
			Cwd:            cwd,
			SystemPrompt:   systemPrompt,
			MaxTurns:       cfg.MaxTurns,
			PermissionMode: "bypassPermissions",
			AllowedTools:   cfg.Tools,
			NoUserSettings: true,
		},
	}

	ch, err := o.bridge.Execute(ctx, req)
	if err != nil {
		onEvent(WorkerEvent{TaskID: task.ID, Type: "error", Message: err.Error()})
		return TaskResult{
			TaskID:     task.ID,
			Success:    false,
			Error:      err.Error(),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}

	var content string
	var costUSD float64
	var numTurns int

	for ev := range ch {
		switch ev.Type {
		case "tool_use":
			onEvent(WorkerEvent{TaskID: task.ID, Type: "progress", ToolName: ev.Name})
		case "assistant":
			if ev.Text != "" {
				content = ev.Text
			}
		case "result":
			if ev.Content != "" {
				content = ev.Content
			}
			costUSD = ev.CostUSD
			numTurns = ev.NumTurns
		case "error":
			errMsg := ev.Message
			if errMsg == "" {
				errMsg = "unknown bridge error"
			}
			onEvent(WorkerEvent{TaskID: task.ID, Type: "error", Message: errMsg})
			return TaskResult{
				TaskID:     task.ID,
				Success:    false,
				Content:    content,
				Error:      errMsg,
				DurationMs: time.Since(start).Milliseconds(),
				CostUSD:    costUSD,
			}
		}
	}

	duration := time.Since(start).Milliseconds()
	onEvent(WorkerEvent{TaskID: task.ID, Type: "done", Message: fmt.Sprintf("Completed in %dms (%d turns)", duration, numTurns)})

	return TaskResult{
		TaskID:     task.ID,
		Content:    content,
		Success:    true,
		DurationMs: duration,
		CostUSD:    costUSD,
	}
}

// ExecutePlan executes all tasks in the plan, respecting dependencies (wave-based).
// It resolves agent config per task and manages worktrees.
func (o *Orchestrator) ExecutePlan(
	ctx context.Context,
	plan *Plan,
	registry *agents.Registry,
	systemPromptBuilder func(task Task, cfg WorkerConfig) string,
	onEvent func(WorkerEvent),
) ([]TaskResult, error) {
	waves, err := plan.ExecutionOrder()
	if err != nil {
		return nil, fmt.Errorf("resolving execution order: %w", err)
	}

	var allResults []TaskResult
	var mu sync.Mutex

	for _, wave := range waves {
		sem := make(chan struct{}, o.config.MaxConcurrentWorkers)
		var wg sync.WaitGroup

		for _, task := range wave {
			wg.Add(1)
			sem <- struct{}{}

			go func(t Task) {
				defer wg.Done()
				defer func() { <-sem }()

				cfg := ResolveAgentConfig(registry, t.Agent)

				// Determine cwd (worktree or repo root)
				cwd := o.config.RepoRoot
				var wt *Worktree
				if t.NeedsWorktree && o.worktree != nil {
					var wtErr error
					wt, wtErr = o.worktree.Create(t.ID, currentBranch(o.config.RepoRoot))
					if wtErr == nil {
						cwd = wt.Path
					}
					// If worktree fails, fall back to repo root
				}

				prompt := systemPromptBuilder(t, cfg)
				result := o.ExecuteTask(ctx, t, cfg, cwd, prompt, onEvent)

				// Cleanup worktree
				if wt != nil {
					if result.Success {
						_ = o.worktree.Merge(wt, currentBranch(o.config.RepoRoot))
					}
					_ = o.worktree.Cleanup(wt)
				}

				mu.Lock()
				allResults = append(allResults, result)
				mu.Unlock()
			}(task)
		}

		wg.Wait()
	}

	return allResults, nil
}

// currentBranch returns the current git branch name.
func currentBranch(repoRoot string) string {
	// Simple implementation — read HEAD
	// In production this would use git rev-parse --abbrev-ref HEAD
	return "HEAD"
}
