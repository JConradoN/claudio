package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureClaudeMd_CreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureClaudeMd(dir); err != nil {
		t.Fatalf("EnsureClaudeMd: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(content), "CLAUDE.md") {
		t.Error("CLAUDE.md doesn't contain expected header")
	}
}

func TestEnsureClaudeMd_DoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	_ = os.WriteFile(path, []byte("# My Custom CLAUDE.md"), 0o644)

	if err := EnsureClaudeMd(dir); err != nil {
		t.Fatalf("EnsureClaudeMd: %v", err)
	}

	content, _ := os.ReadFile(path)
	if string(content) != "# My Custom CLAUDE.md" {
		t.Error("EnsureClaudeMd overwrote existing file")
	}
}

func TestEnsureAgentsMd_CreatesWithSquad(t *testing.T) {
	dir := t.TempDir()
	agents := []AgentSummary{
		{Name: "qa", Description: "test specialist", Tools: []string{"Read", "Bash"}, ReadOnly: false},
		{Name: "reviewer", Description: "code reviewer", Tools: []string{"Read", "Grep"}, ReadOnly: true},
	}

	if err := EnsureAgentsMd(dir, agents); err != nil {
		t.Fatalf("EnsureAgentsMd: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "worker (default)") {
		t.Error("missing default worker row")
	}
	if !strings.Contains(s, "qa") {
		t.Error("missing qa agent row")
	}
	if !strings.Contains(s, "read-only") {
		t.Error("missing read-only type for reviewer")
	}
}

func TestReadFileContent_Missing(t *testing.T) {
	got := ReadFileContent("/nonexistent/path")
	if got != "" {
		t.Errorf("expected empty string for missing file, got %q", got)
	}
}

func TestReadFileContent_Exists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0o644)

	got := ReadFileContent(path)
	if got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}
