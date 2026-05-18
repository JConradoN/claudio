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
