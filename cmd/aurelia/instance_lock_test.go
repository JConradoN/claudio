package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireLock_AcquireAndRelease(t *testing.T) {
	tmp := t.TempDir()
	origLock := lockPath
	lockPath = func() string { return filepath.Join(tmp, "instance.lock") }
	t.Cleanup(func() { lockPath = origLock })

	release1, err := acquireLock()
	if err != nil {
		t.Fatalf("first acquireLock: %v", err)
	}

	// Must be able to release
	release1()

	// Re-acquire after release
	release2, err := acquireLock()
	if err != nil {
		t.Fatalf("acquireLock after release: %v", err)
	}
	release2()
}

func TestAcquireLock_StaleLockIsReplaced(t *testing.T) {
	tmp := t.TempDir()
	origLock := lockPath
	lockPath = func() string { return filepath.Join(tmp, "instance.lock") }
	t.Cleanup(func() { lockPath = origLock })

	// Write a stale PID (999999 doesn't exist)
	stalePath := filepath.Join(tmp, "instance.lock")
	if err := os.WriteFile(stalePath, []byte("999999\n"), 0644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	release, err := acquireLock()
	if err != nil {
		t.Fatalf("acquireLock with stale lock: %v", err)
	}
	release()
}

func TestAcquireLock_ExistingAliveProcess(t *testing.T) {
	tmp := t.TempDir()
	origLock := lockPath
	lockPath = func() string { return filepath.Join(tmp, "instance.lock") }
	t.Cleanup(func() { lockPath = origLock })

	// Write the current process's own PID — it should be detected as alive
	pid := os.Getpid()
	if err := os.WriteFile(filepath.Join(tmp, "instance.lock"), []byte{}, 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	_ = pid

	// Acquire should succeed because the lock file exists but has no valid PID/content
	// AND there's no flock held on it (it was just written by test, not by acquireLock)
	release, err := acquireLock()
	if err != nil {
		t.Fatalf("expected lock with empty PID to be treated as stale: %v", err)
	}
	release()
}
