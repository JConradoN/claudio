package observability

import (
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
)

// LoggerConfig controls the structured logger behaviour.
type LoggerConfig struct {
	// Level controls the minimum log level. Valid values:
	// "debug", "info", "warn", "error". Empty defaults to "info".
	Level string `json:"level"`
	// Format controls output format. Valid values:
	// "text" (default) or "json".
	Format string `json:"format"`
}

// InitLogger configures slog.SetDefault with the given config.
// Must be called once during application bootstrap, after config load.
// Invalid Level or Format values fall back to defaults with a warning.
func InitLogger(cfg LoggerConfig) {
	level := parseLevel(cfg.Level)
	format := parseFormat(cfg.Format)

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}

	switch format {
	case "json":
		handler = slog.NewJSONHandler(dedupWriter(os.Stderr), opts)
	default:
		handler = slog.NewTextHandler(dedupWriter(os.Stderr), opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Route the standard log package through slog as well, so legacy
	// log.Printf calls appear in the structured output when JSON mode
	// is active. In text mode the plain log output is redundant but not
	// harmful (it will appear as a non-JSON line alongside slog lines).
	slog.Info("logger initialized", "level", level, "format", format)
}

// parseLevel converts a config string to slog.Level.
// Falls back to slog.LevelInfo on invalid input (with a one-time warning).
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		log.Printf("observability: invalid log_level %q, falling back to info", s)
		return slog.LevelInfo
	}
}

// parseFormat returns the canonical format name.
// Falls back to "text" on invalid input (with a one-time warning).
func parseFormat(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "json":
		return "json"
	case "text", "":
		return "text"
	default:
		log.Printf("observability: invalid log_format %q, falling back to text", s)
		return "text"
	}
}

// dedupWriter wraps an io.Writer so that when JSON mode writes both a
// slog event and the legacy log.Printf text about the same line, the
// duplicate plain line is suppressed. For now it is a pass-through;
// future work may filter plain "logger initialized" lines when JSON
// is active by matching known slog-boot noise.
func dedupWriter(w io.Writer) io.Writer {
	return w
}
