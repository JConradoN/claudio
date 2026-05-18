package dream

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConsolidationJSON_Valid(t *testing.T) {
	raw := `{"actions":[{"delete_file":"old.md"},{"index_entry":{"filename":"new.md","title":"New","remove":false}}]}`
	ext := parseConsolidationJSON(raw)
	if ext == nil {
		t.Fatal("expected parsed result")
	}
	if len(ext.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(ext.Actions))
	}
	if ext.Actions[0].DeleteFile != "old.md" {
		t.Fatalf("expected delete_file old.md, got %s", ext.Actions[0].DeleteFile)
	}
}

func TestParseConsolidationJSON_EmptyActions(t *testing.T) {
	raw := `{"actions":[]}`
	ext := parseConsolidationJSON(raw)
	if ext != nil {
		t.Fatal("expected nil for empty actions")
	}
}

func TestParseConsolidationJSON_InvalidJSON(t *testing.T) {
	raw := `not json`
	ext := parseConsolidationJSON(raw)
	if ext != nil {
		t.Fatal("expected nil for invalid JSON")
	}
}

func TestParseConsolidationJSON_Fenced(t *testing.T) {
	raw := "```json\n{\"actions\":[{\"delete_file\":\"old.md\"}]}\n```"
	ext := parseConsolidationJSON(raw)
	if ext == nil {
		t.Fatal("expected parsed result from fenced JSON")
	}
	if len(ext.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(ext.Actions))
	}
}

func TestParseConsolidationJSON_CapsActions(t *testing.T) {
	actions := make([]string, maxConsolidationActions+2)
	for i := 0; i < len(actions); i++ {
		actions[i] = `{"delete_file":"f` + strings.Repeat("0", i) + `.md"}`
	}
	raw := `{"actions":[` + strings.Join(actions, ",") + `]}`
	ext := parseConsolidationJSON(raw)
	if ext == nil {
		t.Fatal("expected parsed result")
	}
	if len(ext.Actions) > maxConsolidationActions {
		t.Fatalf("expected at most %d actions, got %d", maxConsolidationActions, len(ext.Actions))
	}
	if len(ext.Actions) != maxConsolidationActions {
		t.Fatalf("expected exactly %d actions (capped), got %d", maxConsolidationActions, len(ext.Actions))
	}
}

func TestConsolidationDelete_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	w, err := newSafeMemoryWriter(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a file to delete
	testFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	applied := applyConsolidationActions(w, []consolidationAction{
		{DeleteFile: "test.md"},
	}, 0, 0, "")
	if applied != 1 {
		t.Fatalf("expected 1 applied action, got %d", applied)
	}
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Fatal("expected file to be deleted")
	}
}

func TestConsolidationDelete_RejectsInvalidFilename(t *testing.T) {
	dir := t.TempDir()
	w, err := newSafeMemoryWriter(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	applied := applyConsolidationActions(w, []consolidationAction{
		{DeleteFile: "../outside.md"},
	}, 0, 0, "")
	if applied != 0 {
		t.Fatal("expected path traversal delete to be rejected")
	}
}

func TestConsolidationIndexEntry_CreatesMEMORY(t *testing.T) {
	dir := t.TempDir()
	w, err := newSafeMemoryWriter(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	applied := applyConsolidationActions(w, []consolidationAction{
		{IndexEntry: &indexEntryOp{Filename: "notes.md", Title: "Notes"}},
	}, 0, 0, "")
	if applied != 1 {
		t.Fatalf("expected 1 applied action, got %d", applied)
	}

	memoryIndex := filepath.Join(dir, "MEMORY.md")
	data, err := os.ReadFile(memoryIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[Notes](notes.md)") {
		t.Fatalf("MEMORY.md missing entry: %s", data)
	}
}

func TestLoadMemoryForConsolidation_ReadsFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fact1.md"), []byte("- fact one\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fact2.md"), []byte("- fact two\n"), 0644); err != nil {
		t.Fatal(err)
	}

	content := loadMemoryForConsolidation(dir)
	if content == "" {
		t.Fatal("expected non-empty content")
	}
	if !strings.Contains(content, "fact1.md") {
		t.Fatal("expected fact1.md in content")
	}
	if !strings.Contains(content, "fact two") {
		t.Fatal("expected fact two in content")
	}
}

func TestLoadMemoryForConsolidation_ExcludesPersonas(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "safe.md"), []byte("safe"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "personas_evil.md"), []byte("evil"), 0644); err != nil {
		t.Fatal(err)
	}

	content := loadMemoryForConsolidation(dir)
	if strings.Contains(content, "personas_evil") {
		t.Fatal("expected personas-prefixed file to be excluded")
	}
	if !strings.Contains(content, "safe") {
		t.Fatal("expected safe.md to be included")
	}
}
