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

// Tracker accumulates token usage per chat thread for auto-reset decisions.
// Usage is scoped to chatID+threadID so different topics in a forum do not
// share token budgets.
type Tracker struct {
	mu    sync.RWMutex
	usage map[SessionKey]*Usage
}

func NewTracker() *Tracker {
	return &Tracker{
		usage: make(map[SessionKey]*Usage),
	}
}

// Add accumulates token usage for a chat thread. Returns the updated total tokens.
func (t *Tracker) Add(chatID int64, threadID int, inputTokens, outputTokens, numTurns int, costUSD float64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := SessionKeyFor(chatID, threadID)
	u, ok := t.usage[key]
	if !ok {
		u = &Usage{}
		t.usage[key] = u
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
func (t *Tracker) RecordUsage(chatID int64, threadID int, numTurns int, costUSD float64, maxTokens int, inputTokens int, outputTokens int) bool {
	realTokens := inputTokens + outputTokens
	if realTokens > 0 {
		// Use real token counts from the SDK
		totalTokens := t.Add(chatID, threadID, inputTokens, outputTokens, numTurns, costUSD)
		return maxTokens > 0 && totalTokens >= maxTokens
	}

	// Fallback: estimate from turns when real tokens aren't provided
	t.mu.Lock()
	defer t.mu.Unlock()
	key := SessionKeyFor(chatID, threadID)
	estimated := numTurns * estimatedTokensPerTurn
	u, ok := t.usage[key]
	if !ok {
		u = &Usage{}
		t.usage[key] = u
	}
	u.EstimatedTokens += estimated
	u.NumTurns += numTurns
	u.CostUSD += costUSD
	totalTokens := u.TotalTokens()
	return maxTokens > 0 && totalTokens >= maxTokens
}

// Get returns the current usage for a chat thread.
func (t *Tracker) Get(chatID int64, threadID int) Usage {
	t.mu.RLock()
	defer t.mu.RUnlock()
	key := SessionKeyFor(chatID, threadID)
	u := t.usage[key]
	if u == nil {
		return Usage{}
	}
	return *u
}

// WarningZone returns (true, percentUsed) when the session's token usage is at
// or above 80% of the maxTokens threshold. Returns (false, 0) when no usage
// has been recorded or the threshold is disabled (maxTokens <= 0).
// Useful for gentle user nudges rather than hard cutoffs.
func (t *Tracker) WarningZone(chatID int64, threadID int, maxTokens int) (bool, int) {
	if maxTokens <= 0 {
		return false, 0
	}
	usage := t.Get(chatID, threadID)
	total := usage.TotalTokens()
	if total == 0 {
		return false, 0
	}
	pct := total * 100 / maxTokens
	return pct >= 80, pct
}

// Clear resets usage tracking for a chat thread.
func (t *Tracker) Clear(chatID int64, threadID int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := SessionKeyFor(chatID, threadID)
	delete(t.usage, key)
}
