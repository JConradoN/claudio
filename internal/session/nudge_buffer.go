package session

import "sync"

// NudgeMessage holds one side of a conversation turn.
type NudgeMessage struct {
	Role    string // "user" or "assistant"
	Content string
}

// NudgeBuffer accumulates conversation turns per chat thread for periodic nudge review.
type NudgeBuffer struct {
	mu       sync.Mutex
	messages map[SessionKey][]NudgeMessage
	turns    map[SessionKey]int
}

// NewNudgeBuffer creates a new buffer.
func NewNudgeBuffer() *NudgeBuffer {
	return &NudgeBuffer{
		messages: make(map[SessionKey][]NudgeMessage),
		turns:    make(map[SessionKey]int),
	}
}

// AddTurn records a user+assistant exchange for the given chat.
func (b *NudgeBuffer) AddTurn(chatID int64, threadID int, userMsg, assistantMsg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := SessionKeyFor(chatID, threadID)
	b.messages[key] = append(b.messages[key],
		NudgeMessage{Role: "user", Content: userMsg},
		NudgeMessage{Role: "assistant", Content: assistantMsg},
	)
	b.turns[key]++
}

// TurnCount returns how many turns have been buffered for the chat.
func (b *NudgeBuffer) TurnCount(chatID int64, threadID int) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.turns[SessionKeyFor(chatID, threadID)]
}

// GetAndReset returns all buffered messages for the chat and resets the buffer.
func (b *NudgeBuffer) GetAndReset(chatID int64, threadID int) []NudgeMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := SessionKeyFor(chatID, threadID)
	msgs := b.messages[key]
	delete(b.messages, key)
	delete(b.turns, key)
	return msgs
}
