package dream

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestDreamLockAcquireAndTouch(t *testing.T) {
	dir := t.TempDir()

	// Acquire creates .dream-lock with our PID
	if err := acquireLock(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath(dir)); err != nil {
		t.Fatalf(".dream-lock should exist after acquireLock: %v", err)
	}

	// touchLock creates .dream-last with current mtime
	touchLock(dir)
	if _, err := os.Stat(lastPath(dir)); err != nil {
		t.Fatalf(".dream-last should exist after touchLock: %v", err)
	}

	// lastDreamTime reads from .dream-last
	if lastDreamTime(dir).IsZero() {
		t.Fatal("lastDreamTime should return non-zero time after touchLock")
	}
	if time.Since(lastDreamTime(dir)) > time.Minute {
		t.Fatalf("lastDreamTime too old: %s", lastDreamTime(dir))
	}

	// .dream-lock still exists (not released yet)
	if _, err := os.Stat(lockPath(dir)); err != nil {
		t.Fatalf(".dream-lock should remain until releaseLock: %v", err)
	}
}

func TestDreamLockReleasePermitsReacquire(t *testing.T) {
	dir := t.TempDir()

	// Acquire lock
	if err := acquireLock(dir); err != nil {
		t.Fatal(err)
	}

	// Release
	releaseLock(dir)

	// .dream-lock should be gone
	if _, err := os.Stat(lockPath(dir)); !os.IsNotExist(err) {
		t.Fatal(".dream-lock should be removed after releaseLock")
	}

	// Reacquire should succeed (same process, after release)
	if err := acquireLock(dir); err != nil {
		t.Fatalf("reacquire after release should succeed: %v", err)
	}
	releaseLock(dir)
}

func TestDreamLockBlocksSameProcessWithoutRelease(t *testing.T) {
	dir := t.TempDir()

	if err := acquireLock(dir); err != nil {
		t.Fatal(err)
	}

	// Second acquire without release must block (same PID, alive)
	err := acquireLock(dir)
	if err == nil {
		t.Fatal("expected acquireLock to fail when lock is held by this process")
		releaseLock(dir) // clean up on unexpected success
		return
	}
	if !strings.Contains(err.Error(), "held by live process") {
		t.Fatalf("expected 'held by live process' error, got: %v", err)
	}
	releaseLock(dir)
}
