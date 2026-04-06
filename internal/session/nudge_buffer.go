package session

import "sync"

// NudgeMessage holds one side of a conversation turn.
type NudgeMessage struct {
	Role    string // "user" or "assistant"
	Content string
}

// NudgeBuffer accumulates conversation turns per chat for periodic nudge review.
type NudgeBuffer struct {
	mu       sync.Mutex
	messages map[int64][]NudgeMessage
	turns    map[int64]int
}

// NewNudgeBuffer creates a new buffer.
func NewNudgeBuffer() *NudgeBuffer {
	return &NudgeBuffer{
		messages: make(map[int64][]NudgeMessage),
		turns:    make(map[int64]int),
	}
}

// AddTurn records a user+assistant exchange for the given chat.
func (b *NudgeBuffer) AddTurn(chatID int64, userMsg, assistantMsg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages[chatID] = append(b.messages[chatID],
		NudgeMessage{Role: "user", Content: userMsg},
		NudgeMessage{Role: "assistant", Content: assistantMsg},
	)
	b.turns[chatID]++
}

// TurnCount returns how many turns have been buffered for the chat.
func (b *NudgeBuffer) TurnCount(chatID int64) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.turns[chatID]
}

// GetAndReset returns all buffered messages for the chat and resets the buffer.
func (b *NudgeBuffer) GetAndReset(chatID int64) []NudgeMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	msgs := b.messages[chatID]
	delete(b.messages, chatID)
	delete(b.turns, chatID)
	return msgs
}
