package continuity

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// noopRedact is a redactFn that returns input unchanged — useful for tests
// where we don't care about secret redaction.
func noopRedact(s string) string { return s }

func TestFormatContinuitySection_NilState_ReturnsEmpty(t *testing.T) {
	got := FormatContinuitySection(nil, noopRedact)
	if got != "" {
		t.Fatalf("expected empty for nil state, got %q", got)
	}
}

func TestFormatContinuitySection_BasicContent(t *testing.T) {
	state := &ConversationState{
		CWD:                  "/repo/aurelia",
		ActiveGoal:           "Review memory continuity failures",
		LastUserIntent:       "Asked for a new analysis of memory gaps",
		LastAssistantSummary: "Identified auto-reset and prompt budget issues",
		LastCheckpoint:       "Status: completed; next step: implement Continuity Engine v1",
		LastRunStatus:        "completed",
		SessionCold:          false,
		LastTools:            "Read, Write",
		ResetReason:          "",
		UpdatedAt:            time.Now(),
	}

	got := FormatContinuitySection(state, noopRedact)

	if got == "" {
		t.Fatal("expected non-empty continuity section")
	}

	// Check structural elements
	if !strings.Contains(got, "## Conversation Continuity") {
		t.Fatal("missing section header")
	}
	if !strings.Contains(got, "<continuity_state_untrusted>") {
		t.Fatal("missing opening untrusted delimiter")
	}
	if !strings.Contains(got, "</continuity_state_untrusted>") {
		t.Fatal("missing closing untrusted delimiter")
	}

	// Check content
	if !strings.Contains(got, "/repo/aurelia") {
		t.Fatal("missing CWD in output")
	}
	if !strings.Contains(got, "Review memory continuity failures") {
		t.Fatal("missing ActiveGoal in output")
	}
	if !strings.Contains(got, "Session: warm") {
		t.Fatal("expected warm session, got cold")
	}
	if !strings.Contains(got, "Last run status: completed") {
		t.Fatal("missing last run status")
	}
}

func TestFormatContinuitySection_ColdSession(t *testing.T) {
	state := &ConversationState{
		CWD:           "/repo",
		LastRunStatus: "timed_out",
		SessionCold:   true,
		ResetReason:   "bridge timeout",
		UpdatedAt:     time.Now(),
	}

	got := FormatContinuitySection(state, noopRedact)

	if !strings.Contains(got, "Session: cold") {
		t.Fatal("expected cold session marker")
	}
	if !strings.Contains(got, "Reset reason: bridge timeout") {
		t.Fatal("missing reset reason")
	}
}

func TestFormatContinuitySection_RedactsSecrets(t *testing.T) {
	state := &ConversationState{
		CWD:            "/repo",
		LastUserIntent: "Use API key sk-proj-abc123def456 and token ghp_xyz",
		UpdatedAt:      time.Now(),
	}

	redactFn := func(s string) string {
		// Simulate API key redaction
		r := strings.NewReplacer(
			"sk-proj-abc123def456", "[KEY_REDACTED]",
			"ghp_xyz", "[TOKEN_REDACTED]",
		)
		return r.Replace(s)
	}

	got := FormatContinuitySection(state, redactFn)

	if strings.Contains(got, "sk-proj-abc123def456") {
		t.Fatal("API key was not redacted in output")
	}
	if !strings.Contains(got, "[KEY_REDACTED]") {
		t.Fatal("expected [KEY_REDACTED] marker after redaction")
	}
}

func TestFormatContinuitySection_EmptyState_ReturnsEmpty(t *testing.T) {
	state := &ConversationState{
		CWD:       "",
		UpdatedAt: time.Now(),
	}
	// Only empty fields — should produce no lines
	got := FormatContinuitySection(state, noopRedact)
	if got != "" {
		t.Fatalf("expected empty for state with no populated fields, got %q", got)
	}
}

func TestCapString_UnderLimit(t *testing.T) {
	s := "hello world"
	got := capString(s, 100)
	if got != s {
		t.Fatalf("capString(%q, 100) = %q, want %q", s, got, s)
	}
}

func TestCapString_OverLimit(t *testing.T) {
	s := "hello world this is long"
	got := capString(s, 10)
	if len(got) > 10 {
		t.Fatalf("capString result length = %d, want <= 10", len(got))
	}
	// Must be valid UTF-8
	if !utf8.ValidString(got) {
		t.Fatalf("capString produced invalid UTF-8: %q", got)
	}
}

func TestCapString_UTF8Boundary(t *testing.T) {
	// Multi-byte characters
	s := "éèêë café 日本"
	got := capString(s, 8)
	// Must be valid UTF-8
	if !utf8.ValidString(got) {
		t.Fatalf("capString produced invalid UTF-8: %q", got)
	}
}

func TestIsRecent_RecentState_ReturnsTrue(t *testing.T) {
	state := &ConversationState{UpdatedAt: time.Now()}
	if !IsRecent(state) {
		t.Fatal("expected IsRecent to be true for current time")
	}
}

func TestIsRecent_OldState_ReturnsFalse(t *testing.T) {
	state := &ConversationState{UpdatedAt: time.Now().Add(-30 * 24 * time.Hour)}
	if IsRecent(state) {
		t.Fatal("expected IsRecent to be false for 30-day old state")
	}
}

func TestIsRecent_NilState_ReturnsFalse(t *testing.T) {
	if IsRecent(nil) {
		t.Fatal("expected IsRecent to be false for nil state")
	}
}

func TestFormatContinuitySection_CapsBlockSize(t *testing.T) {
	// Build a state with very large fields
	state := &ConversationState{
		CWD:                  "/repo",
		LastUserIntent:       strings.Repeat("x", 1000),
		LastAssistantSummary: strings.Repeat("y", 2000),
		LastCheckpoint:       strings.Repeat("z", 3000),
		LastRunStatus:        "completed",
		UpdatedAt:            time.Now(),
	}

	got := FormatContinuitySection(state, noopRedact)
	if len(got) > MaxContinuityBlockChars+500 {
		// Allow some overhead for the wrapper text around the capped block
		t.Fatalf("continuity block length = %d, want near MaxContinuityBlockChars=%d", len(got), MaxContinuityBlockChars)
	}
}

func TestIsRecent_Boundary(t *testing.T) {
	// Just under retention threshold
	state := &ConversationState{UpdatedAt: time.Now().Add(-RetentionThreshold + time.Minute)}
	if !IsRecent(state) {
		t.Fatal("expected IsRecent true for state just under retention threshold")
	}

	// Just over retention threshold
	state2 := &ConversationState{UpdatedAt: time.Now().Add(-RetentionThreshold - time.Minute)}
	if IsRecent(state2) {
		t.Fatal("expected IsRecent false for state just over retention threshold")
	}
}

func TestFreshness_Hot(t *testing.T) {
	// Updated just now
	state := &ConversationState{UpdatedAt: time.Now()}
	if level := Freshness(state); level != FreshnessHot {
		t.Fatalf("expected FreshnessHot for current time, got %d", level)
	}

	// Just under fresh threshold
	state2 := &ConversationState{UpdatedAt: time.Now().Add(-FreshThreshold + time.Second)}
	if level := Freshness(state2); level != FreshnessHot {
		t.Fatalf("expected FreshnessHot for state just under fresh threshold, got %d", level)
	}
}

func TestFreshness_Warm(t *testing.T) {
	// Just over fresh threshold
	state := &ConversationState{UpdatedAt: time.Now().Add(-FreshThreshold - time.Minute)}
	if level := Freshness(state); level != FreshnessWarm {
		t.Fatalf("expected FreshnessWarm for state just over fresh threshold, got %d", level)
	}

	// Just under retention threshold
	state2 := &ConversationState{UpdatedAt: time.Now().Add(-RetentionThreshold + time.Minute)}
	if level := Freshness(state2); level != FreshnessWarm {
		t.Fatalf("expected FreshnessWarm for state just under retention threshold, got %d", level)
	}
}

func TestFreshness_Stale(t *testing.T) {
	// Just over retention threshold
	state := &ConversationState{UpdatedAt: time.Now().Add(-RetentionThreshold - time.Minute)}
	if level := Freshness(state); level != FreshnessStale {
		t.Fatalf("expected FreshnessStale for state just over retention threshold, got %d", level)
	}

	// Very old state
	state2 := &ConversationState{UpdatedAt: time.Now().Add(-30 * 24 * time.Hour)}
	if level := Freshness(state2); level != FreshnessStale {
		t.Fatalf("expected FreshnessStale for 30-day old state, got %d", level)
	}
}

func TestFreshness_NilState_ReturnsStale(t *testing.T) {
	if level := Freshness(nil); level != FreshnessStale {
		t.Fatalf("expected FreshnessStale for nil state, got %d", level)
	}
}

func TestIsFresh_ReturnsTrueForHot(t *testing.T) {
	state := &ConversationState{UpdatedAt: time.Now()}
	if !IsFresh(state) {
		t.Fatal("expected IsFresh true for current time")
	}
}

func TestIsFresh_ReturnsFalseForWarm(t *testing.T) {
	state := &ConversationState{UpdatedAt: time.Now().Add(-FreshThreshold - time.Minute)}
	if IsFresh(state) {
		t.Fatal("expected IsFresh false for warm state")
	}
}

func TestIsFresh_ReturnsFalseForStale(t *testing.T) {
	state := &ConversationState{UpdatedAt: time.Now().Add(-RetentionThreshold - time.Minute)}
	if IsFresh(state) {
		t.Fatal("expected IsFresh false for stale state")
	}
}

func TestIsFresh_NilState_ReturnsFalse(t *testing.T) {
	if IsFresh(nil) {
		t.Fatal("expected IsFresh false for nil state")
	}
}
