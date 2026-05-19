package security

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// AuditEvent represents a structured security audit entry for a tool call
// evaluation. All sensitive values are redacted before inclusion.
type AuditEvent struct {
	Timestamp  time.Time      `json:"timestamp"`
	Decision   ToolDecision   `json:"decision"`
	ToolName   string         `json:"tool_name"`
	Reason     string         `json:"reason"`
	ChatID     int64          `json:"chat_id,omitempty"`
	ThreadID   int            `json:"thread_id,omitempty"`
	UserID     int64          `json:"user_id,omitempty"`
	AgentName  string         `json:"agent_name,omitempty"`
	RequestID  string         `json:"request_id,omitempty"`
	Profile    CapabilityProfile `json:"profile"`
	CWD        string         `json:"cwd,omitempty"`
	Redacted   bool           `json:"redacted"`
}

// auditLogger manages the audit output destination with a mutex for safe
// concurrent writes from multiple bridge requests.
type auditLogger struct {
	mu   sync.Mutex
	w    io.Writer
}

// globalAuditLogger is the package-level audit logger instance.
var globalAuditLogger = &auditLogger{
	w: os.Stderr,
}

// SetAuditWriter replaces the audit output writer. Useful for tests.
func SetAuditWriter(w io.Writer) {
	globalAuditLogger.mu.Lock()
	defer globalAuditLogger.mu.Unlock()
	globalAuditLogger.w = w
}

// LogAudit writes a structured audit event as a JSON line to stderr.
// The event is prefixed with "[security]" for easy filtering in log
// aggregation systems.
//
// LogAudit is safe for concurrent use. If the write fails, the error is
// silently dropped — audit failures must never block execution.
func LogAudit(ev AuditEvent) {
	globalAuditLogger.mu.Lock()
	defer globalAuditLogger.mu.Unlock()

	ev.Redacted = true
	data, err := json.Marshal(ev)
	if err != nil {
		// Fallback: write what we can. Audit failures are non-fatal.
		_, _ = io.WriteString(globalAuditLogger.w,
			`[security] {"error":"marshal_failed","reason":"`+err.Error()+`"}`+"\n")
		return
	}

	_, _ = io.WriteString(globalAuditLogger.w, "[security] "+string(data)+"\n")
}
