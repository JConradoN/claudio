package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// ErrDetachedHEAD is returned when git is in a detached HEAD state and
// the base branch cannot be determined.
var ErrDetachedHEAD = errors.New("detached HEAD: cannot determine base branch")

// ErrInvalidRunID is returned when a runID does not match the expected format.
// newRunID() produces "run" + digits (e.g. "run1", "run42"), and CleanupAll's
// path-to-branch conversion depends on the absence of hyphens in the runID.
var ErrInvalidRunID = errors.New("invalid run ID: must match run<digits> (e.g. run1, run42)")

// runIDRe validates that runID matches the internal expected format.
// See newRunID() — the format is "run" followed by one or more decimal digits.
var runIDRe = regexp.MustCompile(`^run\d+$`)

// Package-level lock registry serializing base-repo mutations across all
// WorktreeManager instances for the same normalized repo root.
// repoLocks grows monotonically — intentional, since a daemon instance
// manages a bounded set of unique repo roots (each entry is ~48 bytes).
var (
	repoLocksMu sync.Mutex
	repoLocks   = map[string]*sync.Mutex{}
)

// repoRootLocker returns the per-repo mutex for the given repo root.
// The global registry lock is only held during map lookup/creation, not
// while running git commands under the returned mutex.
func repoRootLocker(repoRoot string) *sync.Mutex {
	repoLocksMu.Lock()
	lock, ok := repoLocks[repoRoot]
	if !ok {
		lock = &sync.Mutex{}
		repoLocks[repoRoot] = lock
	}
	repoLocksMu.Unlock()
	return lock
}

// WorktreeManager manages git worktrees for isolated worker execution.
// All base-repo-mutating operations (Merge, Cleanup, CleanupAll) are
// serialized across all WorktreeManager instances via a per-repo lock
// registry to prevent concurrent git operations on the same checkout.
type WorktreeManager struct {
	repoRoot string
}

// Worktree represents a created git worktree.
type Worktree struct {
	Path   string // absolute path to the worktree directory
	Branch string // branch name (e.g., "worker/task-1-implement-health")
}

// NewWorktreeManager creates a new WorktreeManager rooted at the given repo.
// The repoRoot is normalized to an absolute path with symlinks resolved,
// ensuring consistent lock lookups across different representations of the
// same directory (relative vs absolute, symlinked vs resolved).
func NewWorktreeManager(repoRoot string) *WorktreeManager {
	abs, err := filepath.Abs(repoRoot)
	if err == nil {
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			repoRoot = resolved
		} else {
			repoRoot = abs
		}
	} else {
		// filepath.Abs failed (extremely rare — CWD deleted or permission issue).
		// Fall back to cleaned input. Lock sharing across instances may not work
		// if another caller supplies an absolute version of the same path.
		repoRoot = filepath.Clean(repoRoot)
	}
	return &WorktreeManager{repoRoot: repoRoot}
}

// ResolveBaseBranch returns the current git branch name, rejecting detached HEAD.
func (wm *WorktreeManager) ResolveBaseBranch() (string, error) {
	return resolveBaseBranch(context.Background(), wm.repoRoot)
}

// resolveBaseBranch is the standalone version that works on any repo root.
// Pass ctx for cancellation; the preflight path uses the caller's context.
func resolveBaseBranch(ctx context.Context, repoRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolving base branch: %w", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return "", ErrDetachedHEAD
	}
	return branch, nil
}

// Create creates a new git worktree with an isolated branch, namespaced under runID.
// Branch format: worker/<runID>/<slug(taskID)>
// Path format:   .worktrees/worker-<runID>-<slug(taskID)>
//
// runID is validated against the internal format (run<digits>) to ensure
// the worktree path stays under .worktrees/ and to maintain the assumptions
// of CleanupAll's path-to-branch conversion. Invalid runIDs are rejected
// before any git commands are executed.
//
// Create does NOT acquire the per-repo lock. Worktree creation is designed
// for concurrent safety (isolated branches, separate directories, independent
// .git/worktrees/ entries). All base-repo-mutating operations (Merge,
// Cleanup, CleanupAll) DO acquire the lock.
func (wm *WorktreeManager) Create(runID, taskID, baseBranch string) (*Worktree, error) {
	if !runIDRe.MatchString(runID) {
		return nil, ErrInvalidRunID
	}
	slug := slugify(taskID)
	branch := fmt.Sprintf("worker/%s/%s", runID, slug)
	wtPath := filepath.Join(wm.repoRoot, ".worktrees", fmt.Sprintf("worker-%s-%s", runID, slug))

	// Defense-in-depth: verify cleaned path stays under .worktrees.
	// runID validation already prevents traversal, but this check
	// catches any future changes that could introduce a gap.
	cleanPath := filepath.Clean(wtPath)
	worktreesDir := filepath.Join(wm.repoRoot, ".worktrees")
	if !strings.HasPrefix(cleanPath, worktreesDir+string(filepath.Separator)) && cleanPath != worktreesDir {
		return nil, fmt.Errorf("worktree path %q escapes .worktrees directory", wtPath)
	}

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
// It first ensures the repo root is on baseBranch and refuses to merge
// if the base tree has uncommitted changes. If the merge itself fails,
// it runs git merge --abort to avoid leaving the repo in merge-conflict state.
//
// Merge acquires the per-repo lock to serialize base-repo mutations across
// all WorktreeManager instances — concurrent Merges from parallel workers
// on the same checkout cannot race.
func (wm *WorktreeManager) Merge(wt *Worktree, baseBranch string) error {
	lock := repoRootLocker(wm.repoRoot)
	lock.Lock()
	defer lock.Unlock()

	// Ensure the repo root is on baseBranch
	checkoutCmd := exec.Command("git", "checkout", baseBranch)
	checkoutCmd.Dir = wm.repoRoot
	if out, err := checkoutCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s: %w\n%s", baseBranch, err, out)
	}

	// Refuse merge if the base tree has uncommitted changes
	paths, err := getDirtyPaths(context.Background(), wm.repoRoot)
	if err != nil {
		return fmt.Errorf("checking dirty state before merge: %w", err)
	}
	if len(paths) > 0 {
		return fmt.Errorf("base tree has uncommitted changes; commit or stash before merging worktree %s", wt.Branch)
	}

	cmd := exec.Command("git", "merge", "--no-ff", wt.Branch, "-m",
		fmt.Sprintf("merge: %s into %s", wt.Branch, baseBranch))
	cmd.Dir = wm.repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		// Roll back partial merge to avoid leaving the repo in a broken state.
		// If abort itself fails, report both errors so the operator can diagnose.
		abortCmd := exec.Command("git", "merge", "--abort")
		abortCmd.Dir = wm.repoRoot
		if abortOut, abortErr := abortCmd.CombinedOutput(); abortErr != nil {
			return fmt.Errorf("git merge %s: %w\nmerge --abort also failed: %v\n%s\nabort output: %s", wt.Branch, err, abortErr, out, abortOut)
		}
		return fmt.Errorf("git merge %s: %w\n%s", wt.Branch, err, out)
	}
	return nil
}

// Cleanup removes the worktree and its temporary branch.
// Acquires the per-repo lock because it mutates git state (branch deletion).
func (wm *WorktreeManager) Cleanup(wt *Worktree) error {
	lock := repoRootLocker(wm.repoRoot)
	lock.Lock()
	defer lock.Unlock()

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

// CleanupAll removes all worktrees matching the worker pattern (recovery)
// and their associated worker branches, best-effort.
// Returns the number of worktree directories found and the first error encountered.
// Acquires the per-repo lock because it mutates git state.
func (wm *WorktreeManager) CleanupAll() (int, error) {
	lock := repoRootLocker(wm.repoRoot)
	lock.Lock()
	defer lock.Unlock()

	pattern := filepath.Join(wm.repoRoot, ".worktrees", "worker-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return 0, fmt.Errorf("listing worktree paths: %w", err)
	}
	var errs []error
	for _, m := range matches {
		cmd := exec.Command("git", "worktree", "remove", "--force", m)
		cmd.Dir = wm.repoRoot
		if err := cmd.Run(); err != nil {
			errs = append(errs, fmt.Errorf("removing worktree %s: %w", m, err))
		}
	}

	// Best-effort branch cleanup: convert path names back to branch names.
	//
	// Path:   .worktrees/worker-<runID>-<slug>
	// Branch: worker/<runID>/<slug>
	//
	// We strip the "worker-" prefix and replace the first remaining hyphen
	// with "/" to reconstruct the branch name. This conversion is safe only
	// because newRunID() produces identifiers without hyphens (e.g. "run1").
	// If the runID format changes, this conversion MUST be updated.
	for _, m := range matches {
		baseName := filepath.Base(m)
		rest := strings.TrimPrefix(baseName, "worker-")
		branch := "worker/" + strings.Replace(rest, "-", "/", 1)
		brCmd := exec.Command("git", "branch", "-D", branch)
		brCmd.Dir = wm.repoRoot
		_ = brCmd.Run() // best-effort
	}

	if len(errs) > 0 {
		return len(matches), fmt.Errorf("cleanup errors: %v", errs)
	}
	return len(matches), nil
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
