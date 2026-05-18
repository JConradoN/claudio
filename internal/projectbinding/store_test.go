package projectbinding

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStore_SetResolveFallbackAndDelete(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	groupKey := ConversationKey{ChatID: 42, ThreadID: 0}
	topicKey := ConversationKey{ChatID: 42, ThreadID: 99}

	if err := store.Set(ctx, ProjectBinding{Key: groupKey, CWD: "/repo/group", ProjectSlug: "-repo-group", Source: BindingManual}); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.Resolve(ctx, topicKey)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || !resolved.Inherited || resolved.Binding.CWD != "/repo/group" {
		t.Fatalf("expected inherited group binding, got %#v", resolved)
	}

	if err := store.Set(ctx, ProjectBinding{Key: topicKey, CWD: "/repo/topic", ProjectSlug: "-repo-topic", Source: BindingManual}); err != nil {
		t.Fatal(err)
	}
	resolved, err = store.Resolve(ctx, topicKey)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.Inherited || resolved.Binding.CWD != "/repo/topic" {
		t.Fatalf("expected topic override, got %#v", resolved)
	}

	if err := store.Delete(ctx, topicKey); err != nil {
		t.Fatal(err)
	}
	resolved, err = store.Resolve(ctx, topicKey)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || !resolved.Inherited || resolved.Binding.CWD != "/repo/group" {
		t.Fatalf("expected fallback after delete, got %#v", resolved)
	}
}

func TestSQLiteStore_ReopenPersistsBinding(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bindings.db")
	key := ConversationKey{ChatID: 7, ThreadID: 3}

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, ProjectBinding{Key: key, CWD: "/repo/aurelia", ProjectSlug: "-repo-aurelia", Source: BindingManual, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	resolved, err := reopened.Resolve(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.Binding.CWD != "/repo/aurelia" {
		t.Fatalf("expected persisted binding, got %#v", resolved)
	}
}
