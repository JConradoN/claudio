package dream

import (
	"sync"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/session"
)

func TestNudgeGuard_DifferentKeys_Concurrent(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{})
	key1 := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 0}
	key2 := session.SessionKey{ChatID: 2, ThreadID: 0, UserID: 0}

	var wg sync.WaitGroup
	wg.Add(2)

	var ok1, ok2 bool

	go func() {
		ok1 = d.tryStartNudge(key1)
		wg.Done()
	}()
	go func() {
		ok2 = d.tryStartNudge(key2)
		wg.Done()
	}()

	wg.Wait()

	if !ok1 {
		t.Error("expected first key to acquire guard")
	}
	if !ok2 {
		t.Error("expected second key to acquire guard concurrently")
	}

	// Cleanup
	d.finishNudge(key1)
	d.finishNudge(key2)
}

func TestNudgeGuard_SameKey_BlockedAfterAcquire(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{})
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 0}

	if !d.tryStartNudge(key) {
		t.Fatal("expected first acquire to succeed")
	}

	if d.tryStartNudge(key) {
		t.Fatal("expected second acquire to be blocked")
	}

	// Cleanup
	d.finishNudge(key)
}

func TestNudgeGuard_SameKey_ReacquireAfterFinish(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{})
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 0}

	if !d.tryStartNudge(key) {
		t.Fatal("expected first acquire to succeed")
	}
	d.finishNudge(key)

	if !d.tryStartNudge(key) {
		t.Fatal("expected re-acquire after finish to succeed")
	}

	d.finishNudge(key)
}

func TestNudgeRateLimit_AllowsFirstRun(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{NudgeMinInterval: 10 * time.Minute})
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 0}

	if !d.nudgeRateOK(key) {
		t.Fatal("expected first run to be allowed (no prior run)")
	}
}

func TestNudgeRateLimit_BlocksAfterRecentRun(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{NudgeMinInterval: 10 * time.Minute})
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 0}

	d.nudgeRecordRun(key)
	if d.nudgeRateOK(key) {
		t.Fatal("expected nudge to be blocked (too soon)")
	}
}

func TestNudgeRateLimit_AllowsAfterInterval(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{NudgeMinInterval: 1 * time.Millisecond})
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 0}

	d.nudgeRecordRun(key)
	time.Sleep(5 * time.Millisecond)
	if !d.nudgeRateOK(key) {
		t.Fatal("expected nudge to be allowed after interval passed")
	}
}

func TestNudgeRateLimit_ZeroIntervalAlwaysAllows(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{NudgeMinInterval: 0})
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 0}

	d.nudgeRecordRun(key)
	if !d.nudgeRateOK(key) {
		t.Fatal("expected nudge to be allowed with zero interval")
	}
}

func TestNudgeGC_RemovesStaleEntries(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{NudgeMinInterval: 1 * time.Millisecond})
	key := session.SessionKey{ChatID: 99, ThreadID: 0, UserID: 0}

	d.nudgeRecordRun(key)
	time.Sleep(5 * time.Millisecond)
	d.nudgeGC()

	// After GC, the entry should be removed and a new run should be allowed
	if !d.nudgeRateOK(key) {
		t.Fatal("expected rate limit entry to be GC'd and allow new run")
	}
}

func TestNudgeGC_KeepsRecentEntries(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{NudgeMinInterval: 10 * time.Minute})
	key := session.SessionKey{ChatID: 99, ThreadID: 0, UserID: 0}

	d.nudgeRecordRun(key)
	d.nudgeGC()

	if d.nudgeRateOK(key) {
		t.Fatal("expected recent entry to survive GC and still block")
	}
}

func TestNudgeGC_ZeroIntervalUsesDefault(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{NudgeMinInterval: 0})
	key := session.SessionKey{ChatID: 99, ThreadID: 0, UserID: 0}

	d.nudgeRecordRun(key)
	d.nudgeGC()

	// With zero interval, default cutoff is 1 hour, so recent entry survives
	if !d.nudgeRateOK(key) {
		t.Fatal("expected zero interval to allow rate (rate limit disabled)")
	}
}

func TestNudgeRateLimit_PerKeyTracking(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{NudgeMinInterval: 10 * time.Minute})
	key1 := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 0}
	key2 := session.SessionKey{ChatID: 2, ThreadID: 0, UserID: 0}

	d.nudgeRecordRun(key1)
	if !d.nudgeRateOK(key2) {
		t.Fatal("expected different key to be unaffected")
	}
	if d.nudgeRateOK(key1) {
		t.Fatal("expected original key to still be blocked")
	}
}
