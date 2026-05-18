package telegram

import (
	"testing"
)

// --- cwdSetTarget tests ---

func TestParseCwdSetTarget_GroupFlag(t *testing.T) {
	t.Parallel()

	target, err := parseCwdSetTarget("--group /repo", 59)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 0 {
		t.Errorf("ThreadID = %d, want 0", target.ThreadID)
	}
	if target.Path != "/repo" {
		t.Errorf("Path = %q, want %q", target.Path, "/repo")
	}
	if target.Scope != "group" {
		t.Errorf("Scope = %q, want %q", target.Scope, "group")
	}
	if !target.Explicit {
		t.Error("Explicit should be true")
	}
}

func TestParseCwdSetTarget_GroupFlagWithWhitespace(t *testing.T) {
	t.Parallel()

	target, err := parseCwdSetTarget("  --group   /Volumes/Dados/Workspace  ", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 0 {
		t.Errorf("ThreadID = %d, want 0", target.ThreadID)
	}
	if target.Path != "/Volumes/Dados/Workspace" {
		t.Errorf("Path = %q, want %q", target.Path, "/Volumes/Dados/Workspace")
	}
	if target.Scope != "group" {
		t.Errorf("Scope = %q, want %q", target.Scope, "group")
	}
	if !target.Explicit {
		t.Error("Explicit should be true")
	}
}

func TestParseCwdSetTarget_GroupFlagMissingPath(t *testing.T) {
	t.Parallel()

	_, err := parseCwdSetTarget("--group", 42)
	if err == nil {
		t.Fatal("expected error for missing path after --group")
	}
}

func TestParseCwdSetTarget_GroupFlagOnlyWhitespace(t *testing.T) {
	t.Parallel()

	_, err := parseCwdSetTarget("--group  ", 42)
	if err == nil {
		t.Fatal("expected error for whitespace-only path after --group")
	}
}

func TestParseCwdSetTarget_TopicFlag(t *testing.T) {
	t.Parallel()

	target, err := parseCwdSetTarget("--topic /my/topic/path", 59)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 59 {
		t.Errorf("ThreadID = %d, want 59", target.ThreadID)
	}
	if target.Path != "/my/topic/path" {
		t.Errorf("Path = %q, want %q", target.Path, "/my/topic/path")
	}
	if target.Scope != "topic" {
		t.Errorf("Scope = %q, want %q", target.Scope, "topic")
	}
	if !target.Explicit {
		t.Error("Explicit should be true")
	}
}

func TestParseCwdSetTarget_TopicFlagMissingPath(t *testing.T) {
	t.Parallel()

	_, err := parseCwdSetTarget("--topic", 99)
	if err == nil {
		t.Fatal("expected error for missing path after --topic")
	}
}

func TestParseCwdSetTarget_TopicFlagWhitespaceOnly(t *testing.T) {
	t.Parallel()

	_, err := parseCwdSetTarget("--topic ", 99)
	if err == nil {
		t.Fatal("expected error for whitespace-only path after --topic")
	}
}

func TestParseCwdSetTarget_NoFlagInTopic(t *testing.T) {
	t.Parallel()

	target, err := parseCwdSetTarget("/repo/path", 59)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 59 {
		t.Errorf("ThreadID = %d, want 59", target.ThreadID)
	}
	if target.Path != "/repo/path" {
		t.Errorf("Path = %q, want %q", target.Path, "/repo/path")
	}
	if target.Scope != "topic" {
		t.Errorf("Scope = %q, want %q", target.Scope, "topic")
	}
	if target.Explicit {
		t.Error("Explicit should be false when no flag")
	}
}

func TestParseCwdSetTarget_NoFlagInGeneralChat(t *testing.T) {
	t.Parallel()

	target, err := parseCwdSetTarget("/repo/path", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 0 {
		t.Errorf("ThreadID = %d, want 0", target.ThreadID)
	}
	if target.Path != "/repo/path" {
		t.Errorf("Path = %q, want %q", target.Path, "/repo/path")
	}
	if target.Scope != "group" {
		t.Errorf("Scope = %q, want %q", target.Scope, "group")
	}
	if target.Explicit {
		t.Error("Explicit should be false when no flag")
	}
}

func TestParseCwdSetTarget_EmptyArgs(t *testing.T) {
	t.Parallel()

	_, err := parseCwdSetTarget("", 0)
	if err == nil {
		t.Fatal("expected error for empty args")
	}
}

func TestParseCwdSetTarget_WhitespaceOnly(t *testing.T) {
	t.Parallel()

	_, err := parseCwdSetTarget("   ", 0)
	if err == nil {
		t.Fatal("expected error for whitespace-only args")
	}
}

func TestParseCwdSetTarget_QuotedPathAfterFlag(t *testing.T) {
	t.Parallel()

	// Backtick-quoted path after --group flag
	target, err := parseCwdSetTarget("--group `/repo with spaces`", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 0 {
		t.Errorf("ThreadID = %d, want 0", target.ThreadID)
	}
	if target.Path != "`/repo with spaces`" {
		t.Errorf("Path = %q, want %q", target.Path, "`/repo with spaces`")
	}
}

func TestParseCwdSetTarget_FlagOnlyInPathPosition(t *testing.T) {
	t.Parallel()

	// --group in middle of string should be treated as path (not parsed as flag)
	target, err := parseCwdSetTarget("/some/path --group", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 42 {
		t.Errorf("ThreadID = %d, want 42", target.ThreadID)
	}
	if target.Path != "/some/path --group" {
		t.Errorf("Path = %q, want %q", target.Path, "/some/path --group")
	}
}

func TestParseCwdSetTarget_GroupprefixNoMatch(t *testing.T) {
	t.Parallel()

	// --grouppath (no space) should NOT match --group flag
	target, err := parseCwdSetTarget("--grouppath", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Path != "--grouppath" {
		t.Errorf("Path = %q, want %q", target.Path, "--grouppath")
	}
	if target.ThreadID != 42 {
		t.Errorf("ThreadID = %d, want 42", target.ThreadID)
	}
}

func TestParseCwdSetTarget_TopicPrefixNoMatch(t *testing.T) {
	t.Parallel()

	// --topicsomething should NOT match --topic flag
	target, err := parseCwdSetTarget("--topicsomething", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Path != "--topicsomething" {
		t.Errorf("Path = %q, want %q", target.Path, "--topicsomething")
	}
}

// --- cwdClearThread tests ---

func TestCwdClearThread_Clear(t *testing.T) {
	t.Parallel()

	threadID, ok, err := cwdClearThread("clear", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if threadID != 42 {
		t.Errorf("threadID = %d, want 42", threadID)
	}
}

func TestCwdClearThread_ClearGroup(t *testing.T) {
	t.Parallel()

	threadID, ok, err := cwdClearThread("clear --group", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if threadID != 0 {
		t.Errorf("threadID = %d, want 0", threadID)
	}
}

func TestCwdClearThread_ClearTopic(t *testing.T) {
	t.Parallel()

	threadID, ok, err := cwdClearThread("clear --topic", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if threadID != 42 {
		t.Errorf("threadID = %d, want 42", threadID)
	}
}

func TestCwdClearThread_ClearUnknownFlag(t *testing.T) {
	t.Parallel()

	_, _, err := cwdClearThread("clear --invalid", 42)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestCwdClearThread_NotClear(t *testing.T) {
	t.Parallel()

	_, ok, err := cwdClearThread("/some/path", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for non-clear args")
	}
}
