package memoryux

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	checkpointFilename    = "current_task.md"
	memoryIndexFilename   = "MEMORY.md"
)

// writeCheckpoint atomically writes the checkpoint file to dir with validations.
// It creates the directory (0700), rejects symlink escapes, and writes
// the checkpoint file (0600) via temp+rename.
func writeCheckpoint(dir, scope, cwd, note string, chatID int64, threadID int) (CheckpointResult, error) {
	if !filepath.IsAbs(dir) {
		return CheckpointResult{}, fmt.Errorf("checkpoint directory must be absolute, got %q", dir)
	}

	// Resolve symlinks if dir exists; create if not
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return CheckpointResult{}, fmt.Errorf("create checkpoint dir: %w", err)
		}
		// Re-resolve after MkdirAll because MkdirAll follows symlinks in
		// parent components, potentially creating the directory at a
		// different resolved path than dir describes lexically.
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			resolvedDir = resolved
		} else {
			resolvedDir = dir
		}
	}

	targetPath := filepath.Join(resolvedDir, checkpointFilename)

	// Check if target already exists and is a symlink escaping the resolved dir
	if existing, err := filepath.EvalSymlinks(targetPath); err == nil {
		if !isSubDirLexical(resolvedDir, existing) {
			return CheckpointResult{}, fmt.Errorf("symlink escape: %s -> %s", targetPath, existing)
		}
	}

	// Check if MEMORY.md already exists and is a symlink escaping the resolved dir
	memoryIndexPath := filepath.Join(resolvedDir, memoryIndexFilename)
	if existing, err := filepath.EvalSymlinks(memoryIndexPath); err == nil {
		if !isSubDirLexical(resolvedDir, existing) {
			return CheckpointResult{}, fmt.Errorf("MEMORY.md symlink escape: %s -> %s", memoryIndexPath, existing)
		}
	}

	// Determine if creating new file
	wasNew := false
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		wasNew = true
	}

	content := buildCheckpointContent(scope, cwd, note, chatID, threadID)

	// Atomic write via temp file + rename
	tmpPath := targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0600); err != nil {
		return CheckpointResult{}, fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return CheckpointResult{}, fmt.Errorf("rename temp: %w", err)
	}

	now := time.Now()

	// Update MEMORY.md index (non-fatal on error so a stale index doesn't
	// destroy the checkpoint itself).
	if idxErr := updateMemoryIndex(resolvedDir, checkpointFilename, "Current Task Checkpoint"); idxErr != nil {
		log.Printf("checkpoint: update MEMORY.md index: %v", idxErr)
	}

	return CheckpointResult{
		Layer:     scope,
		Dir:       resolvedDir,
		Path:      targetPath,
		Created:   wasNew,
		UpdatedAt: now,
	}, nil
}

// buildCheckpointContent returns the deterministic markdown checkpoint body.
func buildCheckpointContent(scope, cwd, note string, chatID int64, threadID int) string {
	cwdLine := cwd
	if cwdLine == "" {
		cwdLine = "none"
	}
	if note == "" {
		note = "_No note provided._"
	}

	ts := time.Now().Format(time.RFC3339)
	return fmt.Sprintf("# Current Task Checkpoint\n"+
		"\n"+
		"Updated: %s\n"+
		"Scope: %s\n"+
		"CWD: %s\n"+
		"\n"+
		"## User Note\n"+
		"%s\n"+
		"\n"+
		"## Known State\n"+
		"- Active memory layer: %s\n"+
		"- Chat: %d, Thread: %d\n"+
		"\n"+
		"## Next Step\n"+
		"- Continue from this checkpoint or update it with `/memory checkpoint <summary>`.\n",
		ts, scope, cwdLine, note, scope, chatID, threadID)
}

// updateMemoryIndex ensures MEMORY.md has a "- [Title](filename.md)" entry.
// Creates the file with a header if it does not exist.
func updateMemoryIndex(dir, filename, title string) error {
	indexPath := filepath.Join(dir, memoryIndexFilename)
	entryLine := fmt.Sprintf("- [%s](%s)", title, filename)

	data, err := os.ReadFile(indexPath)
	if err != nil {
		header := "# Memory Index\n\n"
		return os.WriteFile(indexPath, []byte(header+entryLine+"\n"), 0600)
	}

	// Check line by line if entry already exists
	content := string(data)
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == entryLine {
			return nil
		}
	}

	// Append to existing; ensure trailing newline
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += entryLine + "\n"
	return os.WriteFile(indexPath, []byte(content), 0600)
}

// isSubDirLexical checks containment via clean + relative path comparison.
// Does NOT resolve symlinks — safe for paths that don't exist yet.
func isSubDirLexical(parent, sub string) bool {
	parent = filepath.Clean(parent)
	sub = filepath.Clean(sub)
	if parent == sub {
		return true
	}
	rel, err := filepath.Rel(parent, sub)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}
