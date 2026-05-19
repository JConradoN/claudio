package orchestrator

import (
	"context"
	"fmt"
	"log"
	"log/slog"
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
			Model:           cfg.Model,
			Cwd:             cwd,
			SystemPrompt:    systemPrompt,
			AllowedTools:    cfg.Tools,
			DisallowedTools: cfg.DisallowedTools,
			NoUserSettings:  true,
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
	// gotResult prevents false success when the bridge closes without returning
	// a "result" event. Channel ordering guarantees happens-before: a result
	// event is always observable on the channel before the channel is closed.
	var gotResult bool

	for ev := range ch {
		switch ev.Type {
		case "tool_use":
			onEvent(WorkerEvent{TaskID: task.ID, Type: "progress", ToolName: ev.Name})
		case "assistant":
			if ev.Text != "" {
				content = ev.Text
			}
		case "result":
			gotResult = true
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
	if !gotResult {
		onEvent(WorkerEvent{TaskID: task.ID, Type: "error", Message: "bridge closed without result"})
		return TaskResult{
			TaskID:     task.ID,
			Success:    false,
			Content:    content,
			Error:      "bridge closed without result",
			DurationMs: duration,
			CostUSD:    costUSD,
		}
	}

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
				defer func() {
					if r := recover(); r != nil {
						log.Printf("orchestrator: panic executing task %s: %v", t.ID, r)
						mu.Lock()
						allResults = append(allResults, TaskResult{TaskID: t.ID, Success: false, Error: fmt.Sprintf("panic: %v", r)})
						mu.Unlock()
					}
				}()

				cfg := ResolveAgentConfig(registry, t.Agent)

				// Determine cwd (worktree or repo root)
				cwd := o.config.RepoRoot
				var wt *Worktree
				if t.NeedsWorktree && o.worktree != nil {
					var wtErr error
					wt, wtErr = o.worktree.Create(t.ID, currentBranch(o.config.RepoRoot))
					if wtErr == nil {
						cwd = wt.Path
					} else {
						slog.Warn("orchestrator: worktree creation failed, falling back to repo root", "task", t.ID, "error", wtErr)
						onEvent(WorkerEvent{TaskID: t.ID, Type: "warning", Message: "worktree unavailable, running in repo root"})
					}
				}

				prompt := systemPromptBuilder(t, cfg)
				result := o.ExecuteTask(ctx, t, cfg, cwd, prompt, onEvent)

				// Cleanup worktree
				if wt != nil {
					if result.Success {
						if err := o.worktree.Merge(wt, currentBranch(o.config.RepoRoot)); err != nil {
							log.Printf("orchestrator: worktree merge failed for task %s: %v", t.ID, err)
						}
					}
					if err := o.worktree.Cleanup(wt); err != nil {
						log.Printf("orchestrator: worktree cleanup failed for task %s: %v", t.ID, err)
					}
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
