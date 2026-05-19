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

func TestConsolidationMerge_WritesFactsAndIndex(t *testing.T) {
	dir := t.TempDir()
	w, err := newSafeMemoryWriter(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create source files
	if err := os.WriteFile(filepath.Join(dir, "src1.md"), []byte("- old fact 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src2.md"), []byte("- old fact 2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	applied := applyConsolidationActions(w, []consolidationAction{
		{
			MergeFiles: &mergeOp{
				SourceFiles: []string{"src1.md", "src2.md"},
				IntoFile:    "merged.md",
				Title:       "Merged Notes",
				Facts:       []string{"merged fact 1", "merged fact 2"},
			},
		},
	}, 0, 0, "")
	if applied != 1 {
		t.Fatalf("expected 1 applied action, got %d", applied)
	}

	// Verify target file has merged facts
	data, err := os.ReadFile(filepath.Join(dir, "merged.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "- merged fact 1") {
		t.Fatal("missing merged fact 1")
	}
	if !strings.Contains(content, "- merged fact 2") {
		t.Fatal("missing merged fact 2")
	}

	// Verify source files are removed
	if _, err := os.Stat(filepath.Join(dir, "src1.md")); !os.IsNotExist(err) {
		t.Fatal("expected src1.md to be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "src2.md")); !os.IsNotExist(err) {
		t.Fatal("expected src2.md to be removed")
	}

	// Verify MEMORY.md has entry
	memIndex, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(memIndex), "[Merged Notes](merged.md)") {
		t.Fatalf("MEMORY.md missing entry: %s", memIndex)
	}
}

func TestConsolidationMerge_RejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()

	// Create a file outside the memory root
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.md")
	if err := os.WriteFile(outsideFile, []byte("escape"), 0600); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside the memory root pointing outside
	escapeLink := filepath.Join(dir, "merged.md")
	if err := os.Symlink(outsideFile, escapeLink); err != nil {
		t.Fatal(err)
	}

	w, err := newSafeMemoryWriter(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	applied := applyConsolidationActions(w, []consolidationAction{
		{
			MergeFiles: &mergeOp{
				SourceFiles: []string{},
				IntoFile:    "merged.md", // exists as symlink -> outside
				Facts:       []string{"should be rejected"},
			},
		},
	}, 0, 0, "")
	if applied != 0 {
		t.Fatal("expected merge to be rejected for symlink escape")
	}
}

func TestConsolidationMerge_PrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	w, err := newSafeMemoryWriter(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	applied := applyConsolidationActions(w, []consolidationAction{
		{
			MergeFiles: &mergeOp{
				SourceFiles: []string{},
				IntoFile:    "merged.md",
				Facts:       []string{"private fact"},
			},
		},
	}, 0, 0, "")
	if applied != 1 {
		t.Fatalf("expected 1 applied action, got %d", applied)
	}

	// Check file permissions — files must be owner-only (0600)
	fileInfo, err := os.Stat(filepath.Join(dir, "merged.md"))
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm()&0077 != 0 {
		t.Errorf("merged file should be private (mode&0077 == 0), got %o", fileInfo.Mode().Perm())
	}
}

// --- Tolerant parse tests via parseConsolidationJSONWithError ---

func TestParseConsolidationJSONWithError_ProseBeforeJSON(t *testing.T) {
	raw := `After reviewing the files, I recommend:
{"actions":[{"delete_file":"old.md"}]}
Let me know if you agree.`
	ext, err := parseConsolidationJSONWithError(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ext == nil {
		t.Fatal("expected parsed result from prose-wrapped JSON")
	}
	if len(ext.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(ext.Actions))
	}
}

func TestParseConsolidationJSONWithError_EmptyActions(t *testing.T) {
	raw := `{"actions":[]}`
	ext, err := parseConsolidationJSONWithError(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ext != nil {
		t.Fatal("expected nil for empty actions")
	}
}

func TestParseConsolidationJSONWithError_InvalidReturnsError(t *testing.T) {
	raw := `this is not json`
	ext, err := parseConsolidationJSONWithError(raw)
	if ext != nil {
		t.Fatal("expected nil for invalid JSON")
	}
	if err == nil {
		t.Fatal("expected non-nil error for invalid input")
	}
}

func TestParseConsolidationJSONWithError_FencedWithProse(t *testing.T) {
	raw := "Output:\n```\n{\"actions\":[{\"delete_file\":\"old.md\"}]}\n```\nDone."
	ext, err := parseConsolidationJSONWithError(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ext == nil {
		t.Fatal("expected parsed result from fenced + prose JSON")
	}
	if len(ext.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(ext.Actions))
	}
}

func TestParseConsolidationJSONWithError_UnbalancedBraces(t *testing.T) {
	raw := `{"actions":[{"delete_file":"old.md"}]`
	ext, err := parseConsolidationJSONWithError(raw)
	if ext != nil {
		t.Fatal("expected nil for unbalanced braces")
	}
	if err == nil {
		t.Fatal("expected error for unbalanced braces")
	}
}

func TestParseConsolidationJSONWithError_ErrorOnWhitespaceOnly(t *testing.T) {
	_, err := parseConsolidationJSONWithError("   \n  ")
	if err == nil {
		t.Fatal("expected error for whitespace only")
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
