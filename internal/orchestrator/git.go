package orchestrator

import (
	"fmt"
	"os/exec"
	"strings"
)

// CommitChanges stages all changes and creates a commit with the given message.
func CommitChanges(repoRoot, message string) error {
	// Stage all changes
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = repoRoot
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, out)
	}

	// Check if there's anything to commit
	statusCmd := exec.Command("git", "diff", "--cached", "--quiet")
	statusCmd.Dir = repoRoot
	if err := statusCmd.Run(); err == nil {
		return fmt.Errorf("nothing to commit")
	}

	// Commit
	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Dir = repoRoot
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, out)
	}

	return nil
}

// CreatePR creates a pull request using the gh CLI.
// Returns the PR URL or error.
func CreatePR(repoRoot, title, body, baseBranch string) (string, error) {
	if !IsGHAvailable() {
		return "", fmt.Errorf("gh CLI not available or not authenticated")
	}

	args := []string{"pr", "create", "--title", title, "--body", body}
	if baseBranch != "" {
		args = append(args, "--base", baseBranch)
	}

	cmd := exec.Command("gh", args...)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr create: %w\n%s", err, out)
	}

	return strings.TrimSpace(string(out)), nil
}

// IsGHAvailable checks if the gh CLI is installed and authenticated.
func IsGHAvailable() bool {
	cmd := exec.Command("gh", "auth", "status")
	return cmd.Run() == nil
}

// GetCurrentBranch returns the current git branch name.
func GetCurrentBranch(repoRoot string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "HEAD"
	}
	return strings.TrimSpace(string(out))
}

// PushBranch pushes the current branch to remote with -u flag.
func PushBranch(repoRoot string) error {
	branch := GetCurrentBranch(repoRoot)
	cmd := exec.Command("git", "push", "-u", "origin", branch)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %w\n%s", err, out)
	}
	return nil
}
