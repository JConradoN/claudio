package pipeline

import (
	"testing"

	"github.com/igormaneschy/aurelia/internal/session"
)

func TestTurnContext_SessionKey(t *testing.T) {
	tc := &TurnContext{ChatID: 42, ThreadID: 7, UserID: 100}
	key := tc.SessionKey()
	want := session.SessionKey{ChatID: 42, ThreadID: 7, UserID: 100}
	if key != want {
		t.Fatalf("SessionKey() = %+v, want %+v", key, want)
	}
}

func TestTurnContext_ConversationKey(t *testing.T) {
	tc := &TurnContext{ChatID: 42, ThreadID: 7, UserID: 100}
	key := tc.ConversationKey()
	want := session.ConversationKey{ChatID: 42, ThreadID: 7}
	if key != want {
		t.Fatalf("ConversationKey() = %+v, want %+v", key, want)
	}
}

func TestTurnContext_Logger(t *testing.T) {
	tc := &TurnContext{ChatID: 42, ThreadID: 7, UserID: 100}
	logger := tc.Logger()
	if logger == nil {
		t.Fatal("Logger() returned nil")
	}
}
