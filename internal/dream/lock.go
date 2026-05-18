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

const lockFileName = ".dream-lock"

func lockPath(memoryDir string) string {
	return filepath.Join(memoryDir, lockFileName)
}

// lastDreamTime returns the mtime of the lock file, which represents
// when the last dream completed. Returns zero time if never dreamed.
func lastDreamTime(memoryDir string) time.Time {
	info, err := os.Stat(lockPath(memoryDir))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// acquireLock attempts to write our PID to the lock file.
// Returns error if another live process holds the lock.
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

// touchLock updates the lock file mtime to now, recording when
// the last dream completed.
func touchLock(memoryDir string) {
	now := time.Now()
	_ = os.Chtimes(lockPath(memoryDir), now, now)
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
