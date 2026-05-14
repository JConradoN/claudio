package telegram

import (
	"sync"
	"time"
)

// bridgeOutcome represents the result of processing bridge events.
type bridgeOutcome int

const (
	outcomeSuccess      bridgeOutcome = iota // terminal "result" event
	outcomeLLMError                          // terminal "error" event
	outcomeProcessDeath                      // channel closed without terminal event
)

const (
	failureWindowMax = 3                // max failures before cooldown
	failureWindowDur = 1 * time.Minute  // window to count failures
	cooldownDuration = 30 * time.Second // cooldown period after max failures
)

// bridgeFailureTracker tracks consecutive bridge failures to implement cooldown.
type bridgeFailureTracker struct {
	mu       sync.Mutex
	failures []time.Time // timestamps of recent failures
}

// record adds a failure timestamp and returns true if in cooldown.
func (t *bridgeFailureTracker) record() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.failures = append(t.failures, now)

	// Trim failures outside the window
	cutoff := now.Add(-failureWindowDur)
	start := 0
	for start < len(t.failures) && t.failures[start].Before(cutoff) {
		start++
	}
	t.failures = append([]time.Time(nil), t.failures[start:]...)

	return len(t.failures) >= failureWindowMax
}

// inCooldown returns true if we're in cooldown (recent failures >= max).
func (t *bridgeFailureTracker) inCooldown() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.failures) < failureWindowMax {
		return false
	}

	// In cooldown if last failure was within cooldown duration
	last := t.failures[len(t.failures)-1]
	return time.Since(last) < cooldownDuration
}

// reset clears the failure history after a successful execution.
func (t *bridgeFailureTracker) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures = t.failures[:0]
}
