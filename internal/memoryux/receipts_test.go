package memoryux

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAppendReceipt_CreatesFile(t *testing.T) {
	dir := t.TempDir()

	r := Receipt{
		Time:    time.Now().UTC(),
		Source:  "nudge",
		Applied: 2,
		Total:   3,
		Status:  "applied",
	}

	if err := AppendReceipt(dir, r); err != nil {
		t.Fatalf("AppendReceipt(): %v", err)
	}

	path := filepath.Join(dir, receiptFilename)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat receipt file: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("receipt file is empty")
	}
}

func TestAppendReceipt_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission checks not applicable on Windows")
	}

	dir := t.TempDir()

	r := Receipt{
		Time:   time.Now().UTC(),
		Source: "nudge",
		Status: "noop",
	}

	if err := AppendReceipt(dir, r); err != nil {
		t.Fatalf("AppendReceipt(): %v", err)
	}

	path := filepath.Join(dir, receiptFilename)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat receipt file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Fatalf("receipt file has permissions %o, want 0600", mode)
	}
}

func TestAppendReceipt_DirPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission checks not applicable on Windows")
	}

	base := t.TempDir()
	dir := filepath.Join(base, "sub", "receipts")

	r := Receipt{
		Time:   time.Now().UTC(),
		Source: "dream",
		Status: "applied",
	}

	if err := AppendReceipt(dir, r); err != nil {
		t.Fatalf("AppendReceipt(): %v", err)
	}

	// Check parent dir (should have been created)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat receipt dir: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0700 {
		t.Fatalf("receipt dir has permissions %o, want 0700", mode)
	}
}

func TestLatestReceipt_ReturnsLast(t *testing.T) {
	dir := t.TempDir()

	// Write two receipts
	r1 := Receipt{Time: time.Now().UTC().Add(-time.Hour), Source: "nudge", Applied: 1, Total: 2, Status: "applied"}
	r2 := Receipt{Time: time.Now().UTC(), Source: "dream", Applied: 3, Total: 3, Status: "applied"}

	if err := AppendReceipt(dir, r1); err != nil {
		t.Fatalf("append r1: %v", err)
	}
	if err := AppendReceipt(dir, r2); err != nil {
		t.Fatalf("append r2: %v", err)
	}

	got, err := LatestReceipt(dir)
	if err != nil {
		t.Fatalf("LatestReceipt(): %v", err)
	}
	if got == nil {
		t.Fatal("LatestReceipt() returned nil, want receipt")
	}
	if got.Source != "dream" || got.Applied != 3 || got.Total != 3 {
		t.Fatalf("got %+v, want second receipt (dream, 3/3)", got)
	}
}

func TestLatestReceipt_MissingFile(t *testing.T) {
	dir := t.TempDir()

	got, err := LatestReceipt(dir)
	if err != nil {
		t.Fatalf("LatestReceipt(): %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing file, got %+v", got)
	}
}

func TestLatestReceipt_EmptyFile(t *testing.T) {
	dir := t.TempDir()

	// Create empty file
	path := filepath.Join(dir, receiptFilename)
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	got, err := LatestReceipt(dir)
	if err != nil {
		t.Fatalf("LatestReceipt(): %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for empty file, got %+v", got)
	}
}

func TestLatestReceipt_SkipsTrailingEmptyLines(t *testing.T) {
	dir := t.TempDir()

	r := Receipt{Time: time.Now().UTC(), Source: "nudge", Status: "noop"}
	if err := AppendReceipt(dir, r); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Manually append empty lines
	path := filepath.Join(dir, receiptFilename)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("\n\n\n"); err != nil {
		t.Fatalf("write empty lines: %v", err)
	}
	f.Close()

	got, err := LatestReceipt(dir)
	if err != nil {
		t.Fatalf("LatestReceipt(): %v", err)
	}
	if got == nil || got.Source != "nudge" {
		t.Fatalf("expected nudge receipt, got %+v", got)
	}
}

func TestLatestReceipt_SkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()

	// Write valid, corrupt, valid — should return last valid
	r1 := Receipt{Time: time.Now().UTC(), Source: "nudge", Status: "noop"}
	if err := AppendReceipt(dir, r1); err != nil {
		t.Fatalf("append r1: %v", err)
	}

	path := filepath.Join(dir, receiptFilename)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("{corrupt}\n"); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	f.Close()

	r2 := Receipt{Time: time.Now().UTC(), Source: "dream", Status: "applied", Applied: 4, Total: 5}
	if err := AppendReceipt(dir, r2); err != nil {
		t.Fatalf("append r2: %v", err)
	}

	got, err := LatestReceipt(dir)
	if err != nil {
		t.Fatalf("LatestReceipt(): %v", err)
	}
	if got == nil || got.Source != "dream" {
		t.Fatalf("expected dream receipt, got %+v", got)
	}
}

func TestSanitizeReceiptError_Exported(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "something went wrong", "something went wrong"},
		{"with newlines", "line1\nline2\r\nline3", "line1 line2  line3"},
		{"with tabs", "a\tb", "a b"},
		{"control chars", "a\x00b\x01c", "a b c"},
		{"truncated", strings.Repeat("x", 400), strings.Repeat("x", 300)},
		{"truncated with spaces", "a" + strings.Repeat("b", 400), "a" + strings.Repeat("b", 299)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeReceiptError(tt.in)
			if got != tt.want {
				t.Fatalf("sanitizeReceiptError(%q) = %q (len=%d), want %q (len=%d)",
					tt.in, got, len(got), tt.want, len(tt.want))
			}
		})
	}
}

func TestAppendReceipt_EmptyDir(t *testing.T) {
	err := AppendReceipt("", Receipt{Time: time.Now().UTC(), Source: "test", Status: "error"})
	if err == nil {
		t.Fatal("expected error for empty memoryDir")
	}
}

func TestModelOutputDiagnostic_Empty(t *testing.T) {
	got := ModelOutputDiagnostic("", nil)
	if got != "" {
		t.Fatalf("expected empty for empty input, got %q", got)
	}
}

func TestModelOutputDiagnostic_WithError(t *testing.T) {
	raw := `not json content`
	err := fmt.Errorf("no JSON object found")
	got := ModelOutputDiagnostic(raw, err)
	if !strings.Contains(got, "parse_error=") {
		t.Fatalf("expected parse_error in diagnostic, got %q", got)
	}
	if !strings.Contains(got, "output_len=") {
		t.Fatalf("expected output_len in diagnostic, got %q", got)
	}
}

func TestModelOutputDiagnostic_StartsWithJSON(t *testing.T) {
	raw := `{"key": "value"}`
	got := ModelOutputDiagnostic(raw, nil)
	if !strings.Contains(got, "starts_with_json=true") {
		t.Fatalf("expected starts_with_json=true, got %q", got)
	}
	if !strings.Contains(got, "output_len=") {
		t.Fatalf("expected output_len, got %q", got)
	}
	if strings.Contains(got, "parse_error=") {
		t.Fatalf("unexpected parse_error in diagnostic, got %q", got)
	}
}

func TestModelOutputDiagnostic_Fenced(t *testing.T) {
	raw := "```json\n{\"key\": \"value\"}\n```"
	got := ModelOutputDiagnostic(raw, nil)
	if !strings.Contains(got, "fenced=true") {
		t.Fatalf("expected fenced=true, got %q", got)
	}
}

func TestModelOutputDiagnostic_Sanitized(t *testing.T) {
	raw := "line1\nline2"
	err := fmt.Errorf("some error\nwith newlines")
	got := ModelOutputDiagnostic(raw, err)
	if strings.Contains(got, "\n") {
		t.Fatalf("expected no newlines in diagnostic, got %q", got)
	}
}

func TestModelOutputDiagnostic_Truncated(t *testing.T) {
	long := strings.Repeat("a", 500)
	err := fmt.Errorf("%s", "parse: "+long)
	got := ModelOutputDiagnostic(long, err)
	if len(got) > 300 {
		t.Fatalf("expected diagnostic capped at 300 chars, got %d", len(got))
	}
}

func TestModelOutputDiagnostic_Combined(t *testing.T) {
	raw := "```json\n{\"key\": \"val\"}\n```"
	err := fmt.Errorf("unexpected field")
	got := ModelOutputDiagnostic(raw, err)
	if !strings.Contains(got, "parse_error=") {
		t.Fatalf("expected parse_error, got %q", got)
	}
	if !strings.Contains(got, "fenced=true") {
		t.Fatalf("expected fenced=true, got %q", got)
	}
	if !strings.Contains(got, "output_len=") {
		t.Fatalf("expected output_len, got %q", got)
	}
	if strings.Contains(got, raw) {
		t.Fatalf("diagnostic must not contain raw output")
	}
}

func TestModelOutputDiagnostic_NoStartsWithJSONForFenced(t *testing.T) {
	// Fenced output does NOT start with {
	raw := "```\n{\"key\": \"val\"}\n```"
	got := ModelOutputDiagnostic(raw, nil)
	if strings.Contains(got, "starts_with_json=true") {
		t.Fatalf("fenced output should not have starts_with_json=true, got %q", got)
	}
	if !strings.Contains(got, "fenced=true") {
		t.Fatalf("expected fenced=true for fenced output, got %q", got)
	}
}

func TestLatestReceipt_SkipLastCorrupt(t *testing.T) {
	dir := t.TempDir()

	r := Receipt{Time: time.Now().UTC(), Source: "nudge", Status: "applied", Applied: 1, Total: 1}
	if err := AppendReceipt(dir, r); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Append corrupt line at the end
	path := filepath.Join(dir, receiptFilename)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("{garbage}\n"); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	f.Close()

	got, err := LatestReceipt(dir)
	if err != nil {
		t.Fatalf("LatestReceipt(): %v", err)
	}
	if got == nil || got.Source != "nudge" {
		t.Fatalf("expected nudge receipt from valid line, got %+v", got)
	}
}
