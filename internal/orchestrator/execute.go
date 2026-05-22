package orchestrator

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
)

// runIDCounter produces unique run identifiers for ExecutePlan worktree namespaces.
var runIDCounter int64

// newRunID generates a short filesystem-safe run identifier with no hyphens
// or slashes. This is a constraint: CleanupAll's path-to-branch conversion
// depends on the absence of hyphens in the runID to correctly reconstruct
// branch names from worktree paths.
func newRunID() string {
	c := atomic.AddInt64(&runIDCounter, 1)
	return fmt.Sprintf("run%d", c)
}

// workerSessionCounter produces unique, all-negative synthetic session tuples
// for worker bridge requests. Real app session keys may have a positive or
// negative ChatID (group chats are negative), but ThreadID is always ≥0 and
// UserID is positive or zero (transitional paths). An all-negative
// (ChatID, ThreadID, UserID) tuple is therefore reserved for internal worker
// sessions and cannot collide with real session keys.
var workerSessionCounter int64

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

	// Each worker gets a synthetic session scope where ChatID, ThreadID, and
	// UserID are all negative. Real ChatID can be positive or negative (group
	// chats), but ThreadID is always ≥0 and UserID is positive/zero in this
	// app. An all-negative tuple is therefore reserved for internal workers and
	// cannot collide with real session keys. Session persistence is disabled
	// so parallel workers never share or overwrite each other's state.
	workerID := atomic.AddInt64(&workerSessionCounter, 1)
	nID := -workerID
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
			ChatID:          nID,
			ThreadID:        int(nID),
			UserID:          nID,
			PersistSession:  boolPtr(false),
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
// If any task requires a worktree, the base branch is resolved once and a single
// runID is generated for namespace isolation across all worktrees in this plan.
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

	// Determine if any task requires a worktree
	needsWorktree := planHasWorktree(waves)
	if needsWorktree && o.worktree == nil {
		return nil, fmt.Errorf("worktree not available: no repo root configured")
	}

	// Resolve base branch once if worktrees are needed
	var baseBranch string
	var runID string
	if needsWorktree {
		var bbErr error
		baseBranch, bbErr = resolveBaseBranch(ctx, o.config.RepoRoot)
		if bbErr != nil {
			return nil, fmt.Errorf("resolving base branch for worktree tasks: %w", bbErr)
		}
		runID = newRunID()
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

				// If the task requires a worktree, create one. Failure to create
				// is fatal — we do NOT fall back to the repo root (fail-closed).
				// baseBranch and runID are pre-resolved; they are non-empty when
				// needsWorktree is true.
				cwd := o.config.RepoRoot
				var wt *Worktree
				if t.NeedsWorktree {
					var wtErr error
					wt, wtErr = o.worktree.Create(runID, t.ID, baseBranch)
					if wtErr != nil {
						result := TaskResult{
							TaskID:  t.ID,
							Success: false,
							Error:   fmt.Sprintf("worktree creation failed: %v", wtErr),
						}
						onEvent(WorkerEvent{TaskID: t.ID, Type: "error", Message: result.Error})
						mu.Lock()
						allResults = append(allResults, result)
						mu.Unlock()
						return
					}
					cwd = wt.Path
				}

				prompt := systemPromptBuilder(t, cfg)
				result := o.ExecuteTask(ctx, t, cfg, cwd, prompt, onEvent)

				// Worktree lifecycle: merge successful work, then cleanup.
				// On merge failure the worktree/branch are preserved for manual
				// recovery — do NOT force-delete unmerged changes.
				if wt != nil {
					if result.Success {
						if err := o.worktree.Merge(wt, baseBranch); err != nil {
							// Merge failed: preserve worktree for recovery, mark task
							// as failed with a sanitized user-facing message. The full
							// git error (including paths) stays in the server log.
							log.Printf("orchestrator: worktree merge failed for task %s, worktree preserved at %s: %v", t.ID, wt.Path, err)
							result.Success = false
							result.Error = "merge failed; worktree preserved for recovery"
							// Use a distinct event type ("merge_failed") rather than
							// "error" because ExecuteTask already emitted a "done"
							// event. Re-using "error" would invert the event stream
							// (done → error) and confuse UI consumers.
							onEvent(WorkerEvent{TaskID: t.ID, Type: "merge_failed", Message: result.Error})
							// Do NOT cleanup — worktree/branch left for manual recovery
						} else {
							// Merge succeeded — safe to remove worktree and branch
							if err := o.worktree.Cleanup(wt); err != nil {
								log.Printf("orchestrator: worktree cleanup failed for task %s: %v", t.ID, err)
							}
						}
					} else {
						// Task execution failed — no successful changes to merge.
						// Cleanup is safe because nothing was merged.
						if err := o.worktree.Cleanup(wt); err != nil {
							log.Printf("orchestrator: worktree cleanup failed for task %s: %v", t.ID, err)
						}
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

// planHasWorktree returns true if any task in any wave has NeedsWorktree set.
func planHasWorktree(waves [][]Task) bool {
	for _, wave := range waves {
		for _, t := range wave {
			if t.NeedsWorktree {
				return true
			}
		}
	}
	return false
}

// boolPtr returns a pointer to v for use in optional bool request fields.
func boolPtr(v bool) *bool {
	return &v
}
