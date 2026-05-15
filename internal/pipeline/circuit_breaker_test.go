package pipeline

import (
	"strings"
	"testing"
	"time"
)

func TestCircuitBreaker_Closed(t *testing.T) {
	cb := newCircuitBreaker()
	if cb.State() != CircuitClosed {
		t.Error("new breaker should be closed")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := newCircuitBreaker()
	for i := 0; i < circuitFailureThreshold; i++ {
		if cb.State() == CircuitOpen {
			t.Fatalf("opened too early at failure %d", i)
		}
		cb.RecordFailure()
	}
	if cb.State() != CircuitOpen {
		t.Error("should be open after threshold")
	}
}

func TestCircuitBreaker_NotifiesOnce(t *testing.T) {
	cb := newCircuitBreaker()
	for i := 0; i < circuitFailureThreshold; i++ {
		cb.RecordFailure()
	}
	if !cb.ShouldNotify() {
		t.Error("should notify on first open")
	}
	if cb.ShouldNotify() {
		t.Error("should NOT notify twice")
	}
}

func TestCircuitBreaker_HalfOpenAfterTimeout(t *testing.T) {
	cb := newCircuitBreaker()
	for i := 0; i < circuitFailureThreshold; i++ {
		cb.RecordFailure()
	}
	if cb.State() != CircuitOpen {
		t.Fatal("should be open")
	}

	// Simulate time passing
	cb.mu.Lock()
	cb.openedAt = time.Now().Add(-circuitOpenDuration - time.Second)
	cb.mu.Unlock()

	if cb.State() != CircuitHalfOpen {
		t.Error("should transition to half-open after timeout")
	}
}

func TestCircuitBreaker_ClosesOnHalfOpenSuccess(t *testing.T) {
	cb := newCircuitBreaker()
	for i := 0; i < circuitFailureThreshold; i++ {
		cb.RecordFailure()
	}

	// Force half-open
	cb.mu.Lock()
	cb.openedAt = time.Now().Add(-circuitOpenDuration - time.Second)
	cb.mu.Unlock()

	if cb.State() != CircuitHalfOpen {
		t.Fatal("should be half-open")
	}

	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Error("should close on success in half-open")
	}
}

func TestCircuitBreaker_ReopensOnHalfOpenFailure(t *testing.T) {
	cb := newCircuitBreaker()
	for i := 0; i < circuitFailureThreshold; i++ {
		cb.RecordFailure()
	}

	// Force half-open
	cb.mu.Lock()
	cb.openedAt = time.Now().Add(-circuitOpenDuration - time.Second)
	cb.mu.Unlock()

	if cb.State() != CircuitHalfOpen {
		t.Fatal("should be half-open")
	}

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Error("should reopen on failure in half-open")
	}
}

func TestCircuitBreaker_PruneOldFailures(t *testing.T) {
	cb := newCircuitBreaker()
	// Inject old failures outside the window
	cb.mu.Lock()
	cb.failures = append(cb.failures, time.Now().Add(-circuitFailureWindow-time.Minute))
	cb.failures = append(cb.failures, time.Now().Add(-circuitFailureWindow-time.Minute))
	cb.mu.Unlock()

	// Only 3 new failures should NOT open the circuit
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	if cb.State() != CircuitClosed {
		t.Error("old failures should be pruned; circuit should remain closed")
	}
}

func TestCircuitBreakerRegistry(t *testing.T) {
	reg := newCircuitBreakerRegistry()

	if reg.ShouldSkip("kimi") {
		t.Error("should not skip closed provider")
	}

	// Record failures
	for i := 0; i < circuitFailureThreshold; i++ {
		reg.RecordResult("kimi", false)
	}

	if !reg.ShouldSkip("kimi") {
		t.Error("should skip open provider")
	}
	if reg.ShouldSkip("openrouter") {
		t.Error("should NOT skip unrelated provider")
	}

	msg := reg.NotifyMessage("kimi")
	if msg == "" {
		t.Error("should return notification message")
	}
	if reg.NotifyMessage("kimi") != "" {
		t.Error("should not notify twice")
	}

	// Force half-open by manipulating openedAt
	cb := reg.get("kimi")
	cb.mu.Lock()
	cb.openedAt = time.Now().Add(-circuitOpenDuration - time.Second)
	cb.mu.Unlock()

	if cb.State() != CircuitHalfOpen {
		t.Fatal("should be half-open")
	}

	// Success in half-open closes the circuit
	reg.RecordResult("kimi", true)
	if reg.ShouldSkip("kimi") {
		t.Error("should not skip after recovery")
	}
}

func TestCircuitBreakerRegistry_NotifyMessageContent(t *testing.T) {
	reg := newCircuitBreakerRegistry()
	for i := 0; i < circuitFailureThreshold; i++ {
		reg.RecordResult("kimi", false)
	}
	msg := reg.NotifyMessage("kimi")
	if !strings.Contains(msg, "kimi") {
		t.Error("notification should mention provider")
	}
	if !strings.Contains(msg, "instabilidade") {
		t.Error("notification should mention instability")
	}
}
