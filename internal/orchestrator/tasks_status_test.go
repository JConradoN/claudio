package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateTasksStatus_MarksCompleted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.md")

	initial := `# Tasks

### T1: Create interface

**Done when**:
- [ ] Interface defined
- [ ] Types exported

### T2: Implement service

**Done when**:
- [ ] Implements interface
- [ ] Tests pass
`
	os.WriteFile(path, []byte(initial), 0o644)

	results := []TaskResult{
		{TaskID: "T1", Success: true, DurationMs: 5000},
		{TaskID: "T2", Success: false, Error: "tests failed", DurationMs: 8000},
	}

	if err := UpdateTasksStatus(path, results); err != nil {
		t.Fatalf("UpdateTasksStatus: %v", err)
	}

	content, _ := os.ReadFile(path)
	s := string(content)

	// T1 checkboxes should be marked
	if !strings.Contains(s, "- [x] Interface defined") {
		t.Error("T1 checkbox not marked")
	}
	if !strings.Contains(s, "- [x] Types exported") {
		t.Error("T1 second checkbox not marked")
	}

	// T2 checkboxes should remain unchecked (failed)
	if strings.Contains(s, "- [x] Implements interface") {
		t.Error("T2 checkbox should not be marked (task failed)")
	}

	// Status summary should be appended
	if !strings.Contains(s, "Execution Status") {
		t.Error("missing execution status section")
	}
	if !strings.Contains(s, "✅ Done") {
		t.Error("missing success marker for T1")
	}
	if !strings.Contains(s, "❌ Failed") {
		t.Error("missing failure marker for T2")
	}
}
