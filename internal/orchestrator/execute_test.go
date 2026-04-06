package orchestrator

import (
	"context"
	"sync"
	"testing"

	"github.com/kocar/aurelia/internal/bridge"
)

// fakeBridge implements BridgeExecutor for tests.
type fakeBridge struct {
	results    map[string]*bridge.Event // requestPrompt → terminal event
	defaultEv  *bridge.Event            // fallback for unmatched prompts
	mu         sync.Mutex
}

func newFakeBridge() *fakeBridge {
	return &fakeBridge{results: make(map[string]*bridge.Event)}
}

func (f *fakeBridge) SetResult(prompt string, ev *bridge.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[prompt] = ev
}

func (f *fakeBridge) SetDefault(ev *bridge.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaultEv = ev
}

func (f *fakeBridge) Execute(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error) {
	ch := make(chan bridge.Event, 4)

	f.mu.Lock()
	ev, ok := f.results[req.Prompt]
	fallback := f.defaultEv
	f.mu.Unlock()

	go func() {
		defer close(ch)
		ch <- bridge.Event{Type: "system", SessionID: "test-session"}
		if ok && ev != nil {
			ch <- *ev
		} else if fallback != nil {
			ch <- *fallback
		} else {
			ch <- bridge.Event{Type: "result", Content: "done"}
		}
	}()

	return ch, nil
}

func (f *fakeBridge) ExecuteSync(ctx context.Context, req bridge.Request) (*bridge.Event, error) {
	ch, err := f.Execute(ctx, req)
	if err != nil {
		return nil, err
	}
	var last *bridge.Event
	for ev := range ch {
		ev := ev
		last = &ev
	}
	return last, nil
}

func TestExecuteTask_Success(t *testing.T) {
	fb := newFakeBridge()
	fb.SetResult("implement /health", &bridge.Event{
		Type:    "result",
		Content: "endpoint created",
		CostUSD: 0.05,
	})

	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: t.TempDir()})

	var events []WorkerEvent
	result := o.ExecuteTask(
		context.Background(),
		Task{ID: "1", Description: "implement health", Prompt: "implement /health"},
		DefaultWorkerConfig,
		t.TempDir(),
		"system prompt",
		func(ev WorkerEvent) {
			events = append(events, ev)
		},
	)

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Content != "endpoint created" {
		t.Errorf("content = %q, want 'endpoint created'", result.Content)
	}

	// Should have start + done events
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
	if events[0].Type != "start" {
		t.Errorf("first event type = %q, want start", events[0].Type)
	}
	if events[len(events)-1].Type != "done" {
		t.Errorf("last event type = %q, want done", events[len(events)-1].Type)
	}
}

func TestExecuteTask_Error(t *testing.T) {
	fb := newFakeBridge()
	fb.SetResult("bad task", &bridge.Event{
		Type:    "error",
		Message: "model overloaded",
	})

	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: t.TempDir()})

	result := o.ExecuteTask(
		context.Background(),
		Task{ID: "1", Prompt: "bad task"},
		DefaultWorkerConfig,
		t.TempDir(),
		"prompt",
		func(ev WorkerEvent) {},
	)

	if result.Success {
		t.Fatal("expected failure")
	}
	if result.Error != "model overloaded" {
		t.Errorf("error = %q", result.Error)
	}
}

func TestExecutePlan_TwoWaves(t *testing.T) {
	fb := newFakeBridge()
	fb.SetResult("task 1 prompt", &bridge.Event{Type: "result", Content: "done 1"})
	fb.SetResult("task 2 prompt", &bridge.Event{Type: "result", Content: "done 2"})
	fb.SetResult("task 3 prompt", &bridge.Event{Type: "result", Content: "done 3"})

	o := NewOrchestrator(fb, OrchestratorConfig{
		RepoRoot: t.TempDir(),
	})

	plan := &Plan{Tasks: []Task{
		{ID: "1", Description: "first", Prompt: "task 1 prompt", Agent: "worker"},
		{ID: "2", Description: "second", Prompt: "task 2 prompt", Agent: "worker", DependsOn: []string{"1"}},
		{ID: "3", Description: "third", Prompt: "task 3 prompt", Agent: "worker", DependsOn: []string{"1"}},
	}}

	var events []WorkerEvent
	var mu sync.Mutex

	results, err := o.ExecutePlan(
		context.Background(),
		plan,
		nil, // no registry — uses defaults
		func(task Task, cfg WorkerConfig) string { return "test prompt" },
		func(ev WorkerEvent) {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// All should succeed
	for _, r := range results {
		if !r.Success {
			t.Errorf("task %s failed: %s", r.TaskID, r.Error)
		}
	}
}
