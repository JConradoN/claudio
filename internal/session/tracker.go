package session

import "fmt"

// Usage tracks accumulated token usage and cost for a chat session.
type Usage struct {
	InputTokens     int
	OutputTokens    int
	EstimatedTokens int // turn-based estimate when real tokens are unavailable
	CostUSD         float64
	NumTurns        int
}

// TotalTokens returns the best available display total.
// Uses the larger of real tokens and estimated tokens so /usage remains useful
// when the bridge does not return exact token counts.
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
