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

	// Close releases the store's resources.
	Close() error
}
