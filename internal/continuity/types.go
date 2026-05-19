package continuity

import "time"

// ConversationKey uniquely identifies a conversation state by chat and thread.
type ConversationKey struct {
	ChatID   int64
	ThreadID int
}

// ConversationState is the durable recovery context for a chat/thread pair.
// All text fields SHALL be redacted and length-capped before persistence.
type ConversationState struct {
	ChatID   int64
	ThreadID int
	CWD      string

	ActiveGoal           string
	LastUserIntent       string
	LastAssistantSummary string
	LastCheckpoint       string

	LastRunID     string
	LastRunStatus string
	LastTools     string

	SessionID   string
	SessionCold bool
	ResetReason string

	UpdatedAt time.Time
}

// StatePatch carries optional fields for partial updates. Nil pointer means
// "do not update this field", avoiding accidental zero-value overwrites.
type StatePatch struct {
	CWD                  *string
	ActiveGoal           *string
	LastUserIntent       *string
	LastAssistantSummary *string
	LastCheckpoint       *string
	LastRunID            *string
	LastRunStatus        *string
	LastTools            *string
	SessionID            *string
	SessionCold          *bool
	ResetReason          *string
	UpdatedAt            time.Time
}

// Data caps — all truncation must be rune-safe.
const (
	MaxActiveGoal           = 300
	MaxUserIntent           = 500
	MaxAssistantSummary     = 900
	MaxCheckpoint           = 1200
	MaxTools                = 700
	MaxContinuityBlockChars = 2000
)

// RetentionThreshold is the maximum age of a ConversationState we consider
// fresh enough for automatic prompt injection (7 days).
const RetentionThreshold = 7 * 24 * time.Hour
