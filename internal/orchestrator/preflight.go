package orchestrator

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// PreflightResult contains the result of a preflight check.
type PreflightResult struct {
	BaseBranch  string
	DirtyPaths  []string
	GHAvailable bool
}

// PreflightExecution validates that the repo is safe for worker execution.
// It checks that repoRoot is a valid git repository, resolves the base branch
// (rejecting detached HEAD), checks for dirty files, and optionally checks gh
// CLI availability for PR creation. Git commands support cancellation via ctx.
func (o *Orchestrator) PreflightExecution(ctx context.Context, repoRoot string, createPR bool) (*PreflightResult, error) {
	// 1. Check it's a valid git repository
	if err := checkGitRepo(ctx, repoRoot); err != nil {
		return nil, err
	}

	// 2. Resolve base branch (rejects detached HEAD)
	branch, err := resolveBaseBranch(ctx, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("base branch check: %w", err)
	}

	// 3. Check dirty base tree
	dirtyPaths, err := getDirtyPaths(ctx, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("checking dirty paths: %w", err)
	}
	if len(dirtyPaths) > 0 {
		// Cap displayed paths to avoid overwhelming error messages
		display := dirtyPaths
		if len(display) > 5 {
			display = display[:5]
		}
		return nil, fmt.Errorf(
			"base tree has uncommitted changes (%d files): %s; commit or stash before running plan",
			len(dirtyPaths), strings.Join(display, ", "),
		)
	}

	// 4. Check gh availability if PR creation is requested
	ghAvailable := false
	if createPR {
		ghAvailable = IsGHAvailable()
	}

	return &PreflightResult{
		BaseBranch:  branch,
		DirtyPaths:  dirtyPaths,
		GHAvailable: ghAvailable,
	}, nil
}

// PreflightUserMessage returns a user-safe error message from a preflight error.
// Detailed information (file paths, git output, etc.) is only kept in logs.
func PreflightUserMessage(err error) string {
	if err == nil {
		return ""
	}
	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "not a git repository"):
		return "O diretório informado não é um repositório Git válido."
	case strings.Contains(errStr, "detached HEAD"), strings.Contains(errStr, "base branch"):
		return "O repositório está em detached HEAD. Faça checkout de um branch e tente novamente."
	case strings.Contains(errStr, "uncommitted changes"):
		return "O repositório tem alterações não salvas. Faça commit ou stash antes de executar o plano."
	default:
		return "O repositório não está pronto para execução. Verifique o estado do Git e tente novamente."
	}
}

// checkGitRepo verifies that repoRoot is inside a valid git repository.
func checkGitRepo(ctx context.Context, repoRoot string) error {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-dir")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("not a git repository: %w\n%s", err, out)
	}
	return nil
}

// getDirtyPaths returns a list of paths that have uncommitted changes
// (modified, staged, or untracked), or nil if the tree is clean.
func getDirtyPaths(ctx context.Context, repoRoot string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	raw := strings.TrimRight(string(out), "\n")
	if raw == "" {
		return nil, nil
	}
	lines := strings.Split(raw, "\n")
	var paths []string
	for _, line := range lines {
		// Format: XY filename or XY -> renamed filename
		if len(line) > 3 {
			path := strings.TrimSpace(line[3:])
			if path != "" {
				paths = append(paths, path)
			}
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}
	return paths, nil
}
