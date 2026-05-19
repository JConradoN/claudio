package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/session"
)

func TestLoadMemoryContents_RespectsTotalCap(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("x", 5000)
	for i := 0; i < 20; i++ {
		path := filepath.Join(dir, fmt.Sprintf("memory-%02d.md", i))
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	bc := &Service{memoryDir: dir, memoryCache: newMemoryCache(), sessions: session.NewStore()}
	got := bc.loadMemoryContents(1, 0, nil)

	if len(got) > maxMemoryTotalChars {
		t.Fatalf("memory content length = %d, want <= %d", len(got), maxMemoryTotalChars)
	}
	if !strings.Contains(got, "memória truncada") {
		t.Fatalf("expected total truncation notice in memory content")
	}
}

func TestEffectiveCwd_UsesPersistedBindingWithTopicFallback(t *testing.T) {
	store, err := projectbinding.NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := t.Context()
	if err := store.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 42, ThreadID: 0},
		CWD:         "/repo/group",
		ProjectSlug: "-repo-group",
		Source:      projectbinding.BindingManual,
	}); err != nil {
		t.Fatal(err)
	}

	bc := &Service{bindings: store, sessions: session.NewStore()}
	if got := bc.effectiveCwd(nil, 42, 99); got != "/repo/group" {
		t.Fatalf("effectiveCwd() = %q, want group fallback", got)
	}

	if err := store.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 42, ThreadID: 99},
		CWD:         "/repo/topic",
		ProjectSlug: "-repo-topic",
		Source:      projectbinding.BindingManual,
	}); err != nil {
		t.Fatal(err)
	}
	if got := bc.effectiveCwd(nil, 42, 99); got != "/repo/topic" {
		t.Fatalf("effectiveCwd() = %q, want topic override", got)
	}
}

func TestBuildProjectDocsSection_UsesPersistedBinding(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "AGENTS.md"), []byte("project instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := projectbinding.NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Set(t.Context(), projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 42, ThreadID: 99},
		CWD:         projectDir,
		ProjectSlug: "test-project",
		Source:      projectbinding.BindingManual,
	}); err != nil {
		t.Fatal(err)
	}

	bc := &Service{bindings: store, sessions: session.NewStore()}
	got := bc.buildProjectDocsSection(42, nil, 99)
	if !strings.Contains(got, "project instructions") {
		t.Fatalf("expected persisted binding project docs, got %q", got)
	}
}

func TestLoadMemoryContents_CompactModeIncludesIndexAndCurrentTask(t *testing.T) {
	dir := t.TempDir()

	// Create MEMORY.md index
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# Index\n- [Note 1](note1.md)\n- [Note 2](note2.md)"), 0600); err != nil {
		t.Fatal(err)
	}
	// Create current_task.md (should be included in compact mode)
	if err := os.WriteFile(filepath.Join(dir, "current_task.md"), []byte("Working on feature X"), 0600); err != nil {
		t.Fatal(err)
	}
	// Create several large files that should be skipped in compact mode
	for i := 0; i < 10; i++ {
		content := strings.Repeat("x", 5000) + "\n"
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("large-%02d.md", i)), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	bc := &Service{memoryDir: dir, memoryCache: newMemoryCache(), sessions: session.NewStore()}
	got := bc.loadMemoryDirCompact(dir)

	if got == "" {
		t.Fatal("expected non-empty compact output")
	}

	// Must include MEMORY.md index
	if !strings.Contains(got, "MEMORY.md") {
		t.Fatal("expected MEMORY.md index in compact output, got:", got)
	}

	// Must include current_task.md
	if !strings.Contains(got, "current_task.md") {
		t.Fatal("expected current_task.md in compact output, got:", got)
	}

	// Must include compact mode notice
	if !strings.Contains(got, "compact") && !strings.Contains(got, "Compact") {
		t.Fatal("expected compact mode notice, got:", got)
	}

	// Should NOT include most large files (at most compactExtraFiles non-index files)
	if strings.Count(got, "**large-") > compactExtraFiles {
		t.Fatalf("expected at most %d large files in compact output, got more", compactExtraFiles)
	}
}

func TestLoadMemoryContents_CompactModeStaysUnderBudget(t *testing.T) {
	dir := t.TempDir()

	// Create MEMORY.md index (within maxMemoryIndexChars)
	indexContent := "# Index\n" + strings.Repeat("- entry\n", 100)
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(indexContent), 0600); err != nil {
		t.Fatal(err)
	}
	// Create current_task.md
	if err := os.WriteFile(filepath.Join(dir, "current_task.md"), []byte("Current task"), 0600); err != nil {
		t.Fatal(err)
	}
	// Create 20 large files (each 5KB)
	for i := 0; i < 20; i++ {
		content := strings.Repeat("x", 5000) + "\n"
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("note-%02d.md", i)), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	bc := &Service{memoryDir: dir, memoryCache: newMemoryCache(), sessions: session.NewStore()}
	got := bc.loadMemoryDirCompact(dir)

	// Compact mode should be well under maxMemoryTotalChars
	if len(got) > maxMemoryTotalChars {
		t.Fatalf("compact memory content length = %d, want <= %d", len(got), maxMemoryTotalChars)
	}

	// Compact mode should be under maxMemoryIndexChars + some margin for extras
	// Index content + current_task + up to 3 extra files + notice
	if !strings.Contains(got, "MEMORY.md") {
		t.Fatal("expected MEMORY.md in compact output")
	}
	if !strings.Contains(got, "current_task.md") {
		t.Fatal("expected current_task.md in compact output")
	}
}

func TestLoadMemoryContents_TriggersCompactModeAtThreshold(t *testing.T) {
	dir := t.TempDir()

	// Create a global memory layer large enough to exceed memorySummaryTriggerChars (30KB).
	// 30 files × 1000 chars each = 30KB, plus MEMORY.md = triggers compact mode for next layer
	// while leaving ~9KB of budget for the topic layer compact output.
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# Index\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		content := strings.Repeat("y", 1000) + "\n"
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file-%02d.md", i)), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Create a topic layer that should use compact mode
	topicDir := filepath.Join(dir, "topics", "chat_1", "thread_2")
	if err := os.MkdirAll(topicDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(topicDir, "MEMORY.md"), []byte("# Topic Index\n- item"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(topicDir, "current_task.md"), []byte("Topic task"), 0600); err != nil {
		t.Fatal(err)
	}
	// Use small topic files (2000 chars each) so compact output fits within remaining
	// budget and the compact mode notice survives truncation.
	for i := 0; i < 10; i++ {
		content := strings.Repeat("z", 2000)
		if err := os.WriteFile(filepath.Join(topicDir, fmt.Sprintf("topic-%02d.md", i)), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	bc := &Service{memoryDir: dir, memoryCache: newMemoryCache(), sessions: session.NewStore()}
	got := bc.loadMemoryContents(1, 2, nil)

	// Total should be within budget
	if len(got) > maxMemoryTotalChars {
		t.Fatalf("memory content length = %d, want <= %d", len(got), maxMemoryTotalChars)
	}

	// Global layer appears in full
	if !strings.Contains(got, "MEMORY.md") {
		t.Fatal("expected global MEMORY.md in output")
	}

	// Topic layer should be in compact mode (has compact notice)
	if !strings.Contains(got, "compact") && !strings.Contains(got, "Compact") {
		t.Fatal("expected topic layer in compact mode with notice")
	}
}

func TestLoadMemoryContents_IsolatesProjectPrivateByThread(t *testing.T) {
	root := t.TempDir()
	// PathResolver root is private; use AURELIA_HOME path through New for real resolver.
	t.Setenv("AURELIA_HOME", root)
	resolver, err := runtime.New()
	if err != nil {
		t.Fatal(err)
	}
	cwd := "/repo/aurelia"
	thread10 := resolver.ConversationProjectMemoryDir(cwd, 42, 10)
	thread20 := resolver.ConversationProjectMemoryDir(cwd, 42, 20)
	for _, dir := range []string{thread10, thread20} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(thread10, "note.md"), []byte("thread ten private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(thread20, "note.md"), []byte("thread twenty private"), 0600); err != nil {
		t.Fatal(err)
	}

	sessions := session.NewStore()
	sessions.SetCwd(42, 10, cwd)
	bc := &Service{resolver: resolver, sessions: sessions, memoryCache: newMemoryCache()}
	got := bc.loadMemoryContents(42, 10, nil)
	if !strings.Contains(got, "thread ten private") {
		t.Fatalf("expected thread 10 memory, got %q", got)
	}
	if strings.Contains(got, "thread twenty private") {
		t.Fatalf("thread 20 memory leaked into thread 10: %q", got)
	}
}
