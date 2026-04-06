package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1", "1"},
		{"task-1-implement-health", "task-1-implement-health"},
		{"Task 1: Implement Health", "task-1-implement-health"},
		{"T1", "t1"},
		{"!!!", "task"}, // fallback
		{"a/b/c", "abc"},
	}
	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestWorktreeManager_CreateAndCleanup(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a temp git repo
	repoDir := t.TempDir()
	run(t, repoDir, "git", "init")
	run(t, repoDir, "git", "config", "user.email", "test@test.com")
	run(t, repoDir, "git", "config", "user.name", "Test")
	run(t, repoDir, "git", "checkout", "-b", "main")

	// Need at least one commit for worktree to work
	dummyFile := filepath.Join(repoDir, "README.md")
	_ = os.WriteFile(dummyFile, []byte("# test"), 0o644)
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "init")

	wm := NewWorktreeManager(repoDir)

	// Create worktree
	wt, err := wm.Create("t1", "main")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify worktree directory exists
	if _, err := os.Stat(wt.Path); os.IsNotExist(err) {
		t.Fatal("worktree directory not created")
	}

	// Verify branch exists
	if wt.Branch != "worker/t1" {
		t.Errorf("branch = %q, want worker/t1", wt.Branch)
	}

	// Create a file in the worktree
	testFile := filepath.Join(wt.Path, "test.txt")
	_ = os.WriteFile(testFile, []byte("hello"), 0o644)
	run(t, wt.Path, "git", "add", ".")
	run(t, wt.Path, "git", "commit", "-m", "add test file")

	// Merge back
	if err := wm.Merge(wt, "main"); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Verify merged file exists in main repo
	mainTestFile := filepath.Join(repoDir, "test.txt")
	if _, err := os.Stat(mainTestFile); os.IsNotExist(err) {
		t.Error("merged file not found in main repo")
	}

	// Cleanup
	if err := wm.Cleanup(wt); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	// Verify worktree directory removed
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Error("worktree directory not removed after cleanup")
	}
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
