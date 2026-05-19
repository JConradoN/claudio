package runlog

import "time"

// RunStatus represents the lifecycle state of a pipeline run.
type RunStatus string

const (
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunTimedOut  RunStatus = "timed_out"
	RunCanceled  RunStatus = "canceled"
	RunFailed    RunStatus = "failed"
)

// RunRecord is the full representation of a run journal entry.
type RunRecord struct {
	RunID      string
	ChatID     int64
	ThreadID   int
	RequestID  string
	SessionID  string
	CWD        string
	Prompt     string
	Status     RunStatus
	Checkpoint string
	ToolSummary string
	Error      string
	StartedAt  time.Time
	UpdatedAt  time.Time
	CompletedAt time.Time
}

// RunUpdate carries optional fields to update on an existing run.
type RunUpdate struct {
	RunID      string
	SessionID  *string
	Status     *RunStatus
	Checkpoint *string
	ToolSummary *string
	Error      *string
	CompletedAt *time.Time
}
