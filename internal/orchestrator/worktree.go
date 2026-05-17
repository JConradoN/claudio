package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeManager manages git worktrees for isolated worker execution.
type WorktreeManager struct {
	repoRoot string
}

// Worktree represents a created git worktree.
type Worktree struct {
	Path   string // absolute path to the worktree directory
	Branch string // branch name (e.g., "worker/task-1-implement-health")
}

// NewWorktreeManager creates a new WorktreeManager rooted at the given repo.
func NewWorktreeManager(repoRoot string) *WorktreeManager {
	return &WorktreeManager{repoRoot: repoRoot}
}

// Create creates a new git worktree with an isolated branch.
func (wm *WorktreeManager) Create(taskID string, baseBranch string) (*Worktree, error) {
	slug := slugify(taskID)
	branch := fmt.Sprintf("worker/%s", slug)
	wtPath := filepath.Join(wm.repoRoot, ".worktrees", fmt.Sprintf("worker-%s", slug))

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating worktree parent dir: %w", err)
	}

	cmd := exec.Command("git", "worktree", "add", "-b", branch, wtPath, baseBranch)
	cmd.Dir = wm.repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git worktree add: %w\n%s", err, out)
	}

	return &Worktree{Path: wtPath, Branch: branch}, nil
}

// Merge merges the worktree branch back into the base branch.
func (wm *WorktreeManager) Merge(wt *Worktree, baseBranch string) error {
	cmd := exec.Command("git", "merge", "--no-ff", wt.Branch, "-m",
		fmt.Sprintf("merge: %s into %s", wt.Branch, baseBranch))
	cmd.Dir = wm.repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git merge %s: %w\n%s", wt.Branch, err, out)
	}
	return nil
}

// Cleanup removes the worktree and its temporary branch.
func (wm *WorktreeManager) Cleanup(wt *Worktree) error {
	var errs []error

	// Remove worktree
	rmCmd := exec.Command("git", "worktree", "remove", "--force", wt.Path)
	rmCmd.Dir = wm.repoRoot
	if out, err := rmCmd.CombinedOutput(); err != nil {
		errs = append(errs, fmt.Errorf("git worktree remove: %w\n%s", err, out))
	}

	// Delete temporary branch
	brCmd := exec.Command("git", "branch", "-D", wt.Branch)
	brCmd.Dir = wm.repoRoot
	if out, err := brCmd.CombinedOutput(); err != nil {
		// Branch may already be deleted if merge was fast-forward; not fatal.
		_ = out
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// CleanupAll removes all worktrees matching the worker pattern (recovery).
func (wm *WorktreeManager) CleanupAll() error {
	pattern := filepath.Join(wm.repoRoot, ".worktrees", "worker-*")
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		cmd := exec.Command("git", "worktree", "remove", "--force", m)
		cmd.Dir = wm.repoRoot
		_ = cmd.Run() // best-effort cleanup
	}
	return nil
}

// slugify creates a safe branch-name slug from a task ID.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	// Keep only alphanumeric, hyphens, and dots
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			b.WriteRune(r)
		}
	}
	result := b.String()
	if result == "" {
		return "task"
	}
	return result
}
