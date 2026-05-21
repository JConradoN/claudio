package agentmesh_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/agentmesh"
)

func tempStore(t *testing.T) (*agentmesh.Store, func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	s, err := agentmesh.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, func() { _ = s.Close(); _ = os.Remove(path) }
}

func TestOpenCreatesSchema(t *testing.T) {
	s, cleanup := tempStore(t)
	defer cleanup()
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestCreateAndFinishTask(t *testing.T) {
	s, cleanup := tempStore(t)
	defer cleanup()

	const taskID = "test-task-001"
	if err := s.CreateTask(taskID, "claude", "benchmark", `{"cmd":"abs"}`); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := s.MarkRunning(taskID); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}

	if err := s.FinishTask(taskID, "claude", "done", "benchmark completed"); err != nil {
		t.Fatalf("FinishTask: %v", err)
	}

	summary, err := s.RecentTasksSummary(5)
	if err != nil {
		t.Fatalf("RecentTasksSummary: %v", err)
	}
	if !strings.Contains(summary, "done") {
		t.Errorf("expected 'done' in summary, got: %s", summary)
	}
}

func TestLoadContext_Empty(t *testing.T) {
	s, cleanup := tempStore(t)
	defer cleanup()

	ctx, err := s.LoadContext("", 10)
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	if ctx != "" {
		t.Errorf("expected empty context, got %q", ctx)
	}
}

func TestSharedMemorySummary_Empty(t *testing.T) {
	s, cleanup := tempStore(t)
	defer cleanup()

	summary, err := s.SharedMemorySummary()
	if err != nil {
		t.Fatalf("SharedMemorySummary: %v", err)
	}
	if summary != "(memória vazia)" {
		t.Errorf("unexpected summary: %q", summary)
	}
}

func TestRecentTasksSummary_Empty(t *testing.T) {
	s, cleanup := tempStore(t)
	defer cleanup()

	summary, err := s.RecentTasksSummary(5)
	if err != nil {
		t.Fatalf("RecentTasksSummary: %v", err)
	}
	if summary != "(nenhuma task registrada)" {
		t.Errorf("unexpected summary: %q", summary)
	}
}
