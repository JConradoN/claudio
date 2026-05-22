package runlog

import "context"

// Store persists run lifecycle events for observability and checkpointing.
type Store interface {
	// Start creates a new run journal entry with status=running.
	Start(ctx context.Context, record RunRecord) error

	// Update applies partial updates to an existing run by RunID.
	Update(ctx context.Context, update RunUpdate) error

	// Complete transitions a run to a terminal status, persisting checkpoint and error.
	Complete(ctx context.Context, runID string, status RunStatus, checkpoint, errMsg string) error

	// Latest returns the most recent run for a given chat/thread, or nil if none.
	Latest(ctx context.Context, chatID int64, threadID int) (*RunRecord, error)

	// RecordEvent persists a single run event to the timeline.
	// Best-effort: errors are logged, never block the caller.
	RecordEvent(ctx context.Context, ev RunEvent) error

	// ListEvents returns all events for a run, ordered by timestamp ascending.
	ListEvents(ctx context.Context, runID string) ([]RunEvent, error)

	// GetRun returns a single run by RunID, or nil if not found.
	GetRun(ctx context.Context, runID string) (*RunRecord, error)

	// ListRuns returns recent runs matching optional filters.
	// Limit caps the result set (default 20). When chatID is non-zero,
	// results are scoped to that chat. Results are ordered by started_at DESC.
	ListRuns(ctx context.Context, chatID int64, limit int) ([]RunRecord, error)

	// Metrics returns aggregate operational metrics over a time window.
	Metrics(ctx context.Context, filter MetricsFilter) (*MetricsResult, error)

	// Close releases the store's resources.
	Close() error
}
