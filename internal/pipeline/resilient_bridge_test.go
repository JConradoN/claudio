package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

// fakeBridge is a test double for BridgeExecutor.
type fakeBridge struct {
	calls     []bridge.Request
	responses map[string][]bridge.Event
	defaultEv bridge.Event
	executeFn func(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error)
}

func newFakeBridge() *fakeBridge {
	f := &fakeBridge{responses: make(map[string][]bridge.Event)}
	f.executeFn = f.defaultExecute
	return f
}

func (f *fakeBridge) addResponse(prompt string, events []bridge.Event) {
	f.responses[prompt] = events
}

func (f *fakeBridge) defaultExecute(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error) {
	f.calls = append(f.calls, req)
	ch := make(chan bridge.Event, 16)
	evts, ok := f.responses[req.Prompt]
	if !ok {
		ch <- f.defaultEv
		close(ch)
		return ch, nil
	}
	for _, ev := range evts {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func (f *fakeBridge) Execute(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error) {
	return f.executeFn(ctx, req)
}

func TestResilientBridge_Success(t *testing.T) {
	fb := newFakeBridge()
	fb.addResponse("hello", []bridge.Event{
		{Type: "result", Content: "world"},
	})

	rb := NewResilientBridge(fb, fastConfig())
	req := bridge.Request{Prompt: "hello", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	var notify string
	res := rb.Execute(context.Background(), req, func(msg string) { notify = msg })

	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.UsedFallback {
		t.Error("should not use fallback on success")
	}
	if notify != "" {
		t.Error("should not notify on success")
	}
}

func TestResilientBridge_TransientRetryThenSuccess(t *testing.T) {
	fb := newFakeBridge()
	// First two attempts return error, third succeeds.
	attempt := 0
	fb.defaultEv = bridge.Event{Type: "error", Message: "rate limit exceeded"}

	rb := NewResilientBridge(fb, ResilientConfig{
		MaxRetries:       3,
		RetryBackoffBase: 50 * time.Millisecond,
	})

	req := bridge.Request{Prompt: "retry-test", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	// Override the fake to succeed on third call
	original := fb.executeFn
	fb.executeFn = func(ctx context.Context, r bridge.Request) (<-chan bridge.Event, error) {
		attempt++
		if attempt >= 3 {
			ch := make(chan bridge.Event, 2)
			ch <- bridge.Event{Type: "result", Content: "success after retry"}
			close(ch)
			return ch, nil
		}
		return original(ctx, r)
	}

	res := rb.Execute(context.Background(), req, nil)
	if res.Err != nil {
		t.Fatalf("expected success after retry, got: %v", res.Err)
	}
	if attempt < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", attempt)
	}
}

func TestResilientBridge_CircuitBreakerOpens(t *testing.T) {
	fb := newFakeBridge()
	fb.defaultEv = bridge.Event{Type: "error", Message: "rate limit exceeded"}

	cfg := fastConfig()
	cfg.OpenRouterAPIKey = "sk-test"
	rb := NewResilientBridge(fb, cfg)

	req := bridge.Request{Prompt: "fail", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	// Override to return success on fallback (openrouter)
	original := fb.executeFn
	fb.executeFn = func(ctx context.Context, r bridge.Request) (<-chan bridge.Event, error) {
		if r.Options.Provider == "openrouter" {
			ch := make(chan bridge.Event, 2)
			ch <- bridge.Event{Type: "result", Content: "fallback success"}
			close(ch)
			return ch, nil
		}
		return original(ctx, r)
	}

	// Trigger failures to open circuit
	for i := 0; i < circuitFailureThreshold; i++ {
		rb.Execute(context.Background(), req, nil)
	}

	if rb.BreakerState("kimi") != CircuitOpen {
		t.Fatal("circuit should be open")
	}

	// Next request should skip to fallback
	var notifyMsg string
	res := rb.Execute(context.Background(), req, func(msg string) { notifyMsg = msg })

	if !res.UsedFallback {
		t.Error("should use fallback when circuit is open")
	}
	if notifyMsg == "" {
		t.Error("should notify user about fallback")
	}
}

func fastConfig() ResilientConfig {
	cfg := DefaultResilientConfig()
	cfg.RetryBackoffBase = 10 * time.Millisecond
	return cfg
}

func TestResilientBridge_FallbackWithoutOpenRouterKey(t *testing.T) {
	fb := newFakeBridge()
	fb.defaultEv = bridge.Event{Type: "error", Message: "rate limit exceeded"}

	cfg := fastConfig()
	cfg.OpenRouterAPIKey = "" // disabled
	rb := NewResilientBridge(fb, cfg)

	req := bridge.Request{Prompt: "fail", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	var notifyMsg string
	res := rb.Execute(context.Background(), req, func(msg string) { notifyMsg = msg })

	if res.Err == nil {
		t.Fatal("expected error when fallback is unavailable")
	}
	if !strings.Contains(notifyMsg, "OpenRouter") {
		t.Error("should notify about missing OpenRouter config")
	}
}

func TestResilientBridge_NonRetryableError(t *testing.T) {
	fb := newFakeBridge()
	fb.addResponse("auth-fail", []bridge.Event{
		{Type: "error", Message: "Invalid API key"},
	})

	rb := NewResilientBridge(fb, fastConfig())
	req := bridge.Request{Prompt: "auth-fail", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	var notifyMsg string
	res := rb.Execute(context.Background(), req, func(msg string) { notifyMsg = msg })

	if res.Err == nil {
		t.Fatal("expected error for auth failure")
	}
	if !strings.Contains(notifyMsg, "autenticação") {
		t.Error("should notify about auth error")
	}
	if res.UsedFallback {
		t.Error("should NOT fallback on auth error")
	}
}

func TestResilientBridge_FallbackSuccess(t *testing.T) {
	fb := newFakeBridge()
	fb.addResponse("primary-fail", []bridge.Event{
		{Type: "error", Message: "rate limit exceeded"},
	})
	// Fallback request uses same prompt but different provider — our fake matches by prompt.
	// We'll override Execute to distinguish by provider.

	cfg := fastConfig()
	cfg.OpenRouterAPIKey = "sk-test"
	rb := NewResilientBridge(fb, cfg)

	req := bridge.Request{Prompt: "primary-fail", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	// Override to return success when provider is openrouter
	original := fb.executeFn
	fb.executeFn = func(ctx context.Context, r bridge.Request) (<-chan bridge.Event, error) {
		if r.Options.Provider == "openrouter" {
			ch := make(chan bridge.Event, 2)
			ch <- bridge.Event{Type: "result", Content: "fallback result"}
			close(ch)
			return ch, nil
		}
		return original(ctx, r)
	}

	res := rb.Execute(context.Background(), req, nil)
	if res.Err != nil {
		t.Fatalf("expected fallback success: %v", res.Err)
	}
	if !res.UsedFallback {
		t.Error("should mark fallback as used")
	}
}

func TestResilientBridge_AllRetriesFail(t *testing.T) {
	fb := newFakeBridge()
	fb.addResponse("always-fail", []bridge.Event{
		{Type: "error", Message: "rate limit exceeded"},
	})

	cfg := fastConfig()
	cfg.OpenRouterAPIKey = "sk-test"
	rb := NewResilientBridge(fb, cfg)

	req := bridge.Request{Prompt: "always-fail", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	// Fallback also fails
	fb.executeFn = func(ctx context.Context, r bridge.Request) (<-chan bridge.Event, error) {
		ch := make(chan bridge.Event, 2)
		ch <- bridge.Event{Type: "error", Message: "fallback also down"}
		close(ch)
		return ch, nil
	}

	res := rb.Execute(context.Background(), req, nil)
	if res.Err == nil {
		t.Fatal("expected error when all fail")
	}
	if !strings.Contains(res.Err.Error(), "fallback failed") {
		t.Errorf("expected fallback to be attempted and fail, got: %v", res.Err)
	}
}

func TestResilientBridge_CancelDuringRetry(t *testing.T) {
	fb := newFakeBridge()
	fb.addResponse("slow", []bridge.Event{
		{Type: "error", Message: "rate limit exceeded"},
	})

	rb := NewResilientBridge(fb, ResilientConfig{
		MaxRetries:       3,
		RetryBackoffBase: 10 * time.Second, // long backoff
	})

	req := bridge.Request{Prompt: "slow", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	res := rb.Execute(ctx, req, nil)
	if res.Err != context.Canceled {
		t.Fatalf("expected context canceled, got: %v", res.Err)
	}
}

func TestResilientBridge_ProcessDeathOutcome(t *testing.T) {
	fb := newFakeBridge()
	// Simulate process death: closed channel with no terminal event
	fb.executeFn = func(ctx context.Context, r bridge.Request) (<-chan bridge.Event, error) {
		ch := make(chan bridge.Event)
		close(ch)
		return ch, nil
	}

	rb := NewResilientBridge(fb, fastConfig())
	req := bridge.Request{Prompt: "death", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	res := rb.Execute(context.Background(), req, nil)
	if res.Err == nil {
		t.Fatal("expected error for process death")
	}
}

func TestResilientBridge_FallbackResetsSession(t *testing.T) {
	fb := newFakeBridge()
	fb.addResponse("session-test", []bridge.Event{
		{Type: "error", Message: "rate limit exceeded"},
	})

	cfg := fastConfig()
	cfg.OpenRouterAPIKey = "sk-test"
	rb := NewResilientBridge(fb, cfg)

	req := bridge.Request{
		Prompt: "session-test",
		Options: bridge.RequestOptions{
			Provider: "kimi",
			Model:    "kimi-k2",
			Resume:   "sess-123",
			Continue: true,
		},
	}

	var captured bridge.Request
	original := fb.executeFn
	fb.executeFn = func(ctx context.Context, r bridge.Request) (<-chan bridge.Event, error) {
		if r.Options.Provider == "openrouter" {
			captured = r
			ch := make(chan bridge.Event, 2)
			ch <- bridge.Event{Type: "result", Content: "fallback"}
			close(ch)
			return ch, nil
		}
		return original(ctx, r)
	}

	rb.Execute(context.Background(), req, nil)

	if captured.Options.Resume != "" {
		t.Error("fallback request should clear resume")
	}
	if captured.Options.Continue {
		t.Error("fallback request should clear continue")
	}
}
