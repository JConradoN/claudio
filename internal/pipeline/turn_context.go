package pipeline

import (
	"log/slog"

	"github.com/igormaneschy/aurelia/internal/session"
)

// TurnContext identifies the chat, thread, and user for a single turn.
type TurnContext struct {
	ChatID   int64
	ThreadID int
	UserID   int64 // 0 means not set and must be rejected by gate
}

// SessionKey returns a session-scoped key (includes UserID).
func (tc *TurnContext) SessionKey() session.SessionKey {
	return session.SessionKey{ChatID: tc.ChatID, ThreadID: tc.ThreadID, UserID: tc.UserID}
}

// ConversationKey returns a conversation-scoped key (excludes UserID).
func (tc *TurnContext) ConversationKey() session.ConversationKey {
	return session.ConversationKey{ChatID: tc.ChatID, ThreadID: tc.ThreadID}
}

// Logger returns a structured logger with user/chat/thread attributes.
func (tc *TurnContext) Logger() *slog.Logger {
	return slog.Default().With("user_id", tc.UserID, "chat_id", tc.ChatID, "thread_id", tc.ThreadID)
}
