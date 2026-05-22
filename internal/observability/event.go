package observability

import (
	"context"
	"time"
)

// RunEvent represents a single point-in-time event in a run's timeline.
// Phases are defined as constants in this package.
type RunEvent struct {
	ID           int64     // database auto-increment id (0 for new events)
	RunID        string    // correlation id
	Timestamp    time.Time // when the event occurred
	Phase        string    // one of the Phase* constants
	Level        string    // "info", "warn", "error"
	Message      string    // human-readable description
	MetadataJSON string    // small, redacted JSON blob (max 4096 bytes)
}

// EventLevel constants.
const (
	EventLevelInfo  = "info"
	EventLevelWarn  = "warn"
	EventLevelError = "error"
)

// Recorder is the interface for persisting run lifecycle data and events.
//
// Implementations must be fail-open: errors are logged but never block the
// caller. All methods should use short context timeouts (<500ms) in runtime code.
//
// The existing runlog.Store interface handles Start/Update/Complete/Latest.
// Recorder adds the event timeline methods on top.
type Recorder interface {
	// RecordEvent persists a single run event to the timeline.
	// Implementations should redact and truncate metadata_json before storage.
	RecordEvent(ctx context.Context, ev RunEvent) error

	// ListEvents returns all events for a run, ordered by timestamp.
	ListEvents(ctx context.Context, runID string) ([]RunEvent, error)
}

// --- Helper to build events ---

// NewEvent creates a RunEvent with the current timestamp, info level,
// and empty metadata. Fields that are empty at construction time are set
// by the caller before calling RecordEvent if needed.
func NewEvent(runID, phase, message string) RunEvent {
	return RunEvent{
		RunID:     runID,
		Timestamp: time.Now(),
		Phase:     phase,
		Level:     EventLevelInfo,
		Message:   message,
	}
}

// NewErrorEvent creates an error-level RunEvent.
func NewErrorEvent(runID, phase, message string) RunEvent {
	ev := NewEvent(runID, phase, message)
	ev.Level = EventLevelError
	return ev
}

// NewWarnEvent creates a warn-level RunEvent.
func NewWarnEvent(runID, phase, message string) RunEvent {
	ev := NewEvent(runID, phase, message)
	ev.Level = EventLevelWarn
	return ev
}

// MaxEventMetadataBytes is the maximum allowed size for MetadataJSON.
// Values above this are truncated before storage.
const MaxEventMetadataBytes = 4096
