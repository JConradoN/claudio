package session

import (
	"strings"
	"testing"
)

func TestTracker_AddAndGet(t *testing.T) {
	tr := NewTracker()
	total := tr.Add(1, 1000, 500, 2, 0.05)
	if total != 1500 {
		t.Fatalf("expected 1500 total, got %d", total)
	}
	usage := tr.Get(1)
	if usage.InputTokens != 1000 || usage.OutputTokens != 500 || usage.NumTurns != 2 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	total = tr.Add(1, 500, 200, 1, 0.02)
	if total != 2200 {
		t.Fatalf("expected 2200 total, got %d", total)
	}
}

func TestTracker_RecordUsage_WithRealTokens(t *testing.T) {
	tr := NewTracker()
	// Real tokens: 8000 input + 2000 output = 10000 total
	shouldReset := tr.RecordUsage(1, 5, 0.10, 100000, 8000, 2000)
	if shouldReset {
		t.Fatal("should not reset at 10000 tokens (threshold 100000)")
	}
	usage := tr.Get(1)
	if usage.InputTokens != 8000 {
		t.Fatalf("expected 8000 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 2000 {
		t.Fatalf("expected 2000 output tokens, got %d", usage.OutputTokens)
	}

	// Add more to cross threshold
	shouldReset = tr.RecordUsage(1, 10, 0.50, 100000, 80000, 15000)
	if !shouldReset {
		t.Fatal("should reset at 105000 tokens (threshold 100000)")
	}
}

func TestTracker_RecordUsage_FallbackEstimate(t *testing.T) {
	tr := NewTracker()
	// No real tokens (zero) → falls back to numTurns * 3000
	shouldReset := tr.RecordUsage(1, 5, 0.10, 100000, 0, 0)
	if shouldReset {
		t.Fatal("should not reset at 15000 estimated tokens")
	}
	usage := tr.Get(1)
	if usage.EstimatedTokens != 15000 {
		t.Fatalf("expected 15000 estimated tokens, got %d", usage.EstimatedTokens)
	}
	if usage.InputTokens != 0 {
		t.Fatalf("expected InputTokens=0 for estimate-only, got %d", usage.InputTokens)
	}
	// TotalTokens uses the larger of real vs estimated
	if usage.TotalTokens() != 15000 {
		t.Fatalf("expected TotalTokens=15000, got %d", usage.TotalTokens())
	}
}

func TestTracker_EstimatedTokens_DoNotMixWithReal(t *testing.T) {
	tr := NewTracker()
	// First call: no real tokens → estimated
	tr.RecordUsage(1, 3, 0.05, 100000, 0, 0) // 3 * 3000 = 9000 estimated
	// Second call: has real tokens → these should NOT add to EstimatedTokens
	tr.RecordUsage(1, 5, 0.10, 100000, 10000, 5000)

	usage := tr.Get(1)
	if usage.EstimatedTokens != 9000 {
		t.Fatalf("expected EstimatedTokens=9000, got %d", usage.EstimatedTokens)
	}
	if usage.InputTokens != 10000 {
		t.Fatalf("expected InputTokens=10000, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 5000 {
		t.Fatalf("expected OutputTokens=5000, got %d", usage.OutputTokens)
	}
	// TotalTokens = max(15000, 9000) = 15000
	if usage.TotalTokens() != 15000 {
		t.Fatalf("expected TotalTokens=15000, got %d", usage.TotalTokens())
	}
}

func TestTracker_String_IncludesEstimated(t *testing.T) {
	tr := NewTracker()
	tr.RecordUsage(1, 2, 0.02, 100000, 0, 0) // 6000 estimated
	s := tr.Get(1).String()
	if s == "" {
		t.Fatal("expected non-empty string")
	}
	// Should contain the estimated annotation
	if !strings.Contains(s, "estimado: 6000") {
		t.Fatalf("expected string to contain 'estimado: 6000', got: %s", s)
	}
}

func TestTracker_RecordUsage_RealTokensPreventsPrematureReset(t *testing.T) {
	tr := NewTracker()
	// With estimation: 30 turns * 3000 = 90000 (close to threshold)
	// With real tokens: only 20000 actually used
	// This proves real tokens prevent premature reset
	shouldReset := tr.RecordUsage(1, 30, 1.50, 100000, 15000, 5000)
	if shouldReset {
		t.Fatal("should NOT reset — real tokens are 20000, not 90000 estimated")
	}
	usage := tr.Get(1)
	if usage.TotalTokens() != 20000 {
		t.Fatalf("expected 20000 real tokens, got %d", usage.TotalTokens())
	}
}

func TestTracker_Clear(t *testing.T) {
	tr := NewTracker()
	tr.Add(1, 1000, 0, 1, 0.01)
	tr.Clear(1)
	usage := tr.Get(1)
	if usage.TotalTokens() != 0 {
		t.Fatalf("expected zero after clear, got %d", usage.TotalTokens())
	}
}

func TestTracker_RecordUsageZeroThreshold(t *testing.T) {
	tr := NewTracker()
	shouldReset := tr.RecordUsage(1, 10, 0.50, 0, 5000, 3000)
	if shouldReset {
		t.Fatal("should not reset when maxTokens is 0 (disabled)")
	}
}
