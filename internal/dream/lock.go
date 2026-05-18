package dream

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	lockFileName = ".dream-lock"
	lastFileName = ".dream-last"
)

func lockPath(memoryDir string) string {
	return filepath.Join(memoryDir, lockFileName)
}

func lastPath(memoryDir string) string {
	return filepath.Join(memoryDir, lastFileName)
}

// lastDreamTime returns the mtime of the last-run marker file (.dream-last),
// which represents when the last dream completed. Returns zero time if never dreamed.
func lastDreamTime(memoryDir string) time.Time {
	info, err := os.Stat(lastPath(memoryDir))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// acquireLock attempts to write our PID to the active lock file (.dream-lock).
// Returns error if another live process holds the lock. The caller must call
// releaseLock when done to allow future acquisitions.
func acquireLock(memoryDir string) error {
	path := lockPath(memoryDir)

	// Check existing lock
	data, err := os.ReadFile(path)
	if err == nil {
		pidStr := strings.TrimSpace(string(data))
		if pid, err := strconv.Atoi(pidStr); err == nil {
			if isProcessAlive(pid) {
				return fmt.Errorf("dream lock held by live process %d", pid)
			}
		}
	}

	// Write our PID
	pid := os.Getpid()
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0600); err != nil {
		return fmt.Errorf("write dream lock: %w", err)
	}
	return nil
}

// releaseLock removes the active lock file (.dream-lock) so that subsequent
// acquisitions can succeed. It is safe to call even if the lock file doesn't
// exist or was written by a different PID.
func releaseLock(memoryDir string) {
	_ = os.Remove(lockPath(memoryDir))
}

// touchLock records the current time as the last completion by creating/updating
// the last-run marker file (.dream-last). The file's mtime is the signal;
// content is unused. This does not affect active lock state.
func touchLock(memoryDir string) {
	_ = os.WriteFile(lastPath(memoryDir), nil, 0600)
}

// isProcessAlive checks if a process with the given PID exists.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
