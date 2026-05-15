package pipeline

import (
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

const (
	circuitFailureThreshold = 5
	circuitFailureWindow    = 2 * time.Minute
	circuitOpenDuration     = 5 * time.Minute
)

// circuitBreaker prevents retry storms by opening when a provider fails repeatedly.
type circuitBreaker struct {
	mu        sync.Mutex
	state     CircuitState
	failures  []time.Time
	openedAt  time.Time
	notified  bool // user was notified once per open cycle
}

func newCircuitBreaker() *circuitBreaker {
	return &circuitBreaker{state: CircuitClosed}
}

// State returns the current circuit state (thread-safe).
func (cb *circuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.maybeTransition()
	return cb.state
}

// RecordSuccess transitions closed on half-open success.
func (cb *circuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen {
		cb.state = CircuitClosed
		cb.failures = cb.failures[:0]
		cb.notified = false
		return
	}
	if cb.state == CircuitClosed {
		// Clear old failures outside the window.
		cb.pruneFailures(time.Now())
	}
}

// RecordFailure increments the failure count and may open the circuit.
func (cb *circuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	if cb.state == CircuitHalfOpen {
		cb.state = CircuitOpen
		cb.openedAt = now
		cb.notified = false
		return
	}

	cb.failures = append(cb.failures, now)
	cb.pruneFailures(now)

	if len(cb.failures) >= circuitFailureThreshold {
		cb.state = CircuitOpen
		cb.openedAt = now
		cb.notified = false
	}
}

// ShouldNotify returns true once per open cycle when the user should be informed.
func (cb *circuitBreaker) ShouldNotify() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state != CircuitOpen || cb.notified {
		return false
	}
	cb.notified = true
	return true
}

// maybeTransition handles automatic half-open transition without holding the lock
// across I/O (caller already holds the lock).
func (cb *circuitBreaker) maybeTransition() {
	if cb.state != CircuitOpen {
		return
	}
	if time.Since(cb.openedAt) >= circuitOpenDuration {
		cb.state = CircuitHalfOpen
		cb.notified = false
	}
}

// pruneFailures removes entries older than the failure window.
func (cb *circuitBreaker) pruneFailures(now time.Time) {
	cutoff := now.Add(-circuitFailureWindow)
	idx := len(cb.failures)
	for i, t := range cb.failures {
		if t.After(cutoff) {
			idx = i
			break
		}
	}
	cb.failures = cb.failures[idx:]
}

// circuitBreakerRegistry holds a breaker per provider.
type circuitBreakerRegistry struct {
	mu       sync.RWMutex
	breakers map[string]*circuitBreaker
}

func newCircuitBreakerRegistry() *circuitBreakerRegistry {
	return &circuitBreakerRegistry{breakers: make(map[string]*circuitBreaker)}
}

func (r *circuitBreakerRegistry) get(provider string) *circuitBreaker {
	r.mu.RLock()
	cb, ok := r.breakers[provider]
	r.mu.RUnlock()
	if ok {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	cb, ok = r.breakers[provider]
	if ok {
		return cb
	}
	cb = newCircuitBreaker()
	r.breakers[provider] = cb
	return cb
}

// ShouldSkip returns true when the provider's circuit is open and the request should go to fallback.
func (r *circuitBreakerRegistry) ShouldSkip(provider string) bool {
	return r.get(provider).State() == CircuitOpen
}

// RecordResult updates the breaker based on success/failure.
func (r *circuitBreakerRegistry) RecordResult(provider string, success bool) {
	cb := r.get(provider)
	if success {
		cb.RecordSuccess()
	} else {
		cb.RecordFailure()
	}
}

// NotifyMessage returns the user notification message once per open cycle, or empty string.
func (r *circuitBreakerRegistry) NotifyMessage(provider string) string {
	cb := r.get(provider)
	if !cb.ShouldNotify() {
		return ""
	}
	return "⚠️ Provider " + provider + " está com instabilidade. Usando alternativa temporariamente."
}
