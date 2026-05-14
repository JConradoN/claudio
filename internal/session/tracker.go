package session

import (
	"fmt"
	"sync"
)

const estimatedTokensPerTurn = 3000

// Usage tracks accumulated token usage and cost for a chat session.
type Usage struct {
	InputTokens     int
	OutputTokens    int
	EstimatedTokens int // turn-based estimate when real tokens are unavailable
	CostUSD         float64
	NumTurns        int
}

// TotalTokens returns the gate value for auto-reset decisions.
// Uses the larger of real tokens and estimated tokens to avoid underestimation
// (which could prevent timely reset) or overestimation (which could trigger
// premature reset when real tokens are available).
func (u Usage) TotalTokens() int {
	realTokens := u.InputTokens + u.OutputTokens
	if u.EstimatedTokens > realTokens {
		return u.EstimatedTokens
	}
	return realTokens
}

func (u Usage) String() string {
	base := fmt.Sprintf("Tokens: %d (in: %d, out: %d) | Turns: %d | Cost: $%.4f",
		u.TotalTokens(), u.InputTokens, u.OutputTokens, u.NumTurns, u.CostUSD)
	if u.EstimatedTokens > 0 {
		base += fmt.Sprintf(" (estimado: %d)", u.EstimatedTokens)
	}
	return base
}

// Tracker accumulates token usage per chat for auto-reset decisions.
type Tracker struct {
	mu    sync.RWMutex
	usage map[int64]*Usage
}

func NewTracker() *Tracker {
	return &Tracker{
		usage: make(map[int64]*Usage),
	}
}

// Add accumulates token usage for a chat. Returns the updated total tokens.
func (t *Tracker) Add(chatID int64, inputTokens, outputTokens, numTurns int, costUSD float64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	u, ok := t.usage[chatID]
	if !ok {
		u = &Usage{}
		t.usage[chatID] = u
	}
	u.InputTokens += inputTokens
	u.OutputTokens += outputTokens
	u.NumTurns += numTurns
	u.CostUSD += costUSD
	return u.TotalTokens()
}

// RecordUsage tracks real token usage from the bridge result event.
// Uses real input/output tokens when available (from SDK usage field),
// falls back to turn-based estimation only when real tokens are zero.
// Returns true if the session should be reset (threshold exceeded).
func (t *Tracker) RecordUsage(chatID int64, numTurns int, costUSD float64, maxTokens int, inputTokens int, outputTokens int) bool {
	realTokens := inputTokens + outputTokens
	if realTokens > 0 {
		// Use real token counts from the SDK
		totalTokens := t.Add(chatID, inputTokens, outputTokens, numTurns, costUSD)
		return maxTokens > 0 && totalTokens >= maxTokens
	}

	// Fallback: estimate from turns when real tokens aren't provided
	t.mu.Lock()
	estimated := numTurns * estimatedTokensPerTurn
	u, ok := t.usage[chatID]
	if !ok {
		u = &Usage{}
		t.usage[chatID] = u
	}
	u.EstimatedTokens += estimated
	u.NumTurns += numTurns
	u.CostUSD += costUSD
	totalTokens := u.TotalTokens()
	t.mu.Unlock()
	return maxTokens > 0 && totalTokens >= maxTokens
}

// Get returns the current usage for a chat.
func (t *Tracker) Get(chatID int64) Usage {
	t.mu.RLock()
	defer t.mu.RUnlock()
	u := t.usage[chatID]
	if u == nil {
		return Usage{}
	}
	return *u
}

// Clear resets usage tracking for a chat.
func (t *Tracker) Clear(chatID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.usage, chatID)
}
