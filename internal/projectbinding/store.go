package projectbinding

import (
	"context"
	"fmt"
	"time"
)

// ConversationKey identifies a Telegram conversation independently from LLM
// session lifetime. Forum topics use ThreadID; private/non-forum chats use 0.
type ConversationKey struct {
	ChatID   int64
	ThreadID int
}

func (k ConversationKey) String() string {
	return fmt.Sprintf("%d:%d", k.ChatID, k.ThreadID)
}

// BindingSource records how a project binding was created.
type BindingSource string

const (
	BindingManual        BindingSource = "manual"
	BindingConfirmedAuto BindingSource = "confirmed_auto"
)

// ProjectBinding is the persisted project choice for one conversation key.
type ProjectBinding struct {
	Key         ConversationKey
	CWD         string
	ProjectSlug string
	Source      BindingSource
	CreatedBy   int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastUsedAt  time.Time
}

// ResolvedBinding is the result of topic -> group fallback resolution.
type ResolvedBinding struct {
	Binding   *ProjectBinding
	Inherited bool
	SourceKey ConversationKey
}

// Store persists project bindings outside volatile LLM sessions.
type Store interface {
	Get(ctx context.Context, key ConversationKey) (*ProjectBinding, error)
	Resolve(ctx context.Context, key ConversationKey) (*ResolvedBinding, error)
	Set(ctx context.Context, binding ProjectBinding) error
	Delete(ctx context.Context, key ConversationKey) error
	Touch(ctx context.Context, key ConversationKey) error
	ListByUser(ctx context.Context, userID int64, limit int) ([]ProjectBinding, error)
	Close() error
}
