package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// captureSlog runs fn with slog output redirected to a buffer.
// Returns the captured output as a string.
func captureSlog(t *testing.T, format string, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}

	switch format {
	case "json":
		handler = slog.NewJSONHandler(&buf, opts)
	default:
		handler = slog.NewTextHandler(&buf, opts)
	}

	old := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(old)

	fn()
	return buf.String()
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"invalid", slog.LevelInfo}, // fallback
		{"unknown", slog.LevelInfo}, // fallback
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := parseLevel(tc.input)
			if got != tc.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"json", "json"},
		{"JSON", "json"},
		{"text", "text"},
		{"", "text"},
		{"invalid", "text"}, // fallback
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := parseFormat(tc.input)
			if got != tc.want {
				t.Errorf("parseFormat(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestJSONOutputIncludesRunID(t *testing.T) {
	output := captureSlog(t, "json", func() {
		slog.Info("test event",
			"run_id", "run-123",
			"chat_id", int64(42),
			"thread_id", 0,
			"user_id", int64(100),
			"phase", "telegram_received",
		)
	})

	if !strings.Contains(output, "run-123") {
		t.Fatalf("JSON output should contain run_id: %s", output)
	}
	if !strings.Contains(output, "telegram_received") {
		t.Fatalf("JSON output should contain phase: %s", output)
	}

	// Verify it's valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, output)
	}
	if parsed["run_id"] != "run-123" {
		t.Fatalf("run_id = %v, want run-123", parsed["run_id"])
	}
}

func TestLevelFiltering(t *testing.T) {
	tests := []struct {
		name     string
		levelCfg string
		logFn    func()
		wantLog  bool
	}{
		{
			name:     "debug_logged_when_debug_level",
			levelCfg: "debug",
			logFn:    func() { slog.Debug("debug message") },
			wantLog:  true,
		},
		{
			name:     "debug_suppressed_when_info_level",
			levelCfg: "info",
			logFn:    func() { slog.Debug("debug message") },
			wantLog:  false,
		},
		{
			name:     "info_logged_when_info_level",
			levelCfg: "info",
			logFn:    func() { slog.Info("info message") },
			wantLog:  true,
		},
		{
			name:     "warn_logged_when_error_level",
			levelCfg: "error",
			logFn:    func() { slog.Warn("warn message") },
			wantLog:  false,
		},
		{
			name:     "error_logged_when_error_level",
			levelCfg: "error",
			logFn:    func() { slog.Error("error message") },
			wantLog:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Save and restore original logger.
			old := slog.Default()
			defer slog.SetDefault(old)

			// Set up handler with configured level.
			level := parseLevel(tc.levelCfg)
			var buf bytes.Buffer
			handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})
			slog.SetDefault(slog.New(handler))

			tc.logFn()

			hasLog := strings.Contains(buf.String(), "message")
			if tc.wantLog && !hasLog {
				t.Errorf("expected log output, got none")
			}
			if !tc.wantLog && hasLog {
				t.Errorf("expected no log output, got: %s", buf.String())
			}
		})
	}
}

func TestNewEvent(t *testing.T) {
	ev := NewEvent("run-abc", PhaseBridgeRequestStarted, "starting bridge request")
	if ev.RunID != "run-abc" {
		t.Fatalf("RunID = %q, want run-abc", ev.RunID)
	}
	if ev.Phase != PhaseBridgeRequestStarted {
		t.Fatalf("Phase = %q, want %q", ev.Phase, PhaseBridgeRequestStarted)
	}
	if ev.Level != EventLevelInfo {
		t.Fatalf("Level = %q, want info", ev.Level)
	}
	if ev.Message != "starting bridge request" {
		t.Fatalf("Message = %q, want starting bridge request", ev.Message)
	}
	if ev.Timestamp.IsZero() {
		t.Fatal("Timestamp should be set")
	}
}

func TestNewErrorEvent(t *testing.T) {
	ev := NewErrorEvent("run-abc", PhaseRunFailed, "bridge timeout")
	if ev.Level != EventLevelError {
		t.Fatalf("Level = %q, want error", ev.Level)
	}
}
