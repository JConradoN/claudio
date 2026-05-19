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

func TestSQLiteStore_ListByUser_ReturnsBindingsForUserOrdered(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Two bindings for user 42, one for user 99
	now := time.Now()
	insert := func(chatID int64, userID int64, cwd string, tOffset time.Duration) {
		err := store.Set(ctx, ProjectBinding{
			Key:       ConversationKey{ChatID: chatID, ThreadID: 0},
			CWD:       cwd,
			ProjectSlug: "-" + cwd,
			Source:    BindingManual,
			CreatedBy: userID,
			CreatedAt: now.Add(tOffset),
		})
		if err != nil {
			t.Fatal(err)
		}
		// Manually bump last_used_at for ordering
		_, err = store.db.ExecContext(ctx,
			`UPDATE conversation_project_binding SET last_used_at = ? WHERE chat_id = ? AND thread_id = 0`,
			now.Add(tOffset).Unix(), chatID)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert(1, 42, "/repo/alpha", -3*time.Hour)
	insert(2, 42, "/repo/beta", -1*time.Hour)
	insert(3, 42, "/repo/gamma", -2*time.Hour)
	insert(4, 99, "/repo/other", -1*time.Hour)

	result, err := store.ListByUser(ctx, 42, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 bindings for user 42, got %d", len(result))
	}
	// Ordered by last_used_at DESC: beta (-1h), gamma (-2h), alpha (-3h)
	if result[0].CWD != "/repo/beta" {
		t.Fatalf("expected first binding /repo/beta, got %q", result[0].CWD)
	}
	if result[1].CWD != "/repo/gamma" {
		t.Fatalf("expected second binding /repo/gamma, got %q", result[1].CWD)
	}
	if result[2].CWD != "/repo/alpha" {
		t.Fatalf("expected third binding /repo/alpha, got %q", result[2].CWD)
	}

	// No bindings for user 999
	result, err = store.ListByUser(ctx, 999, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 bindings for user 999, got %d", len(result))
	}
}

func TestSQLiteStore_ListByUser_RespectsLimitAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// 10 rows: 5 unique CWDs + 5 duplicates of "/repo/common"
	for i := 0; i < 10; i++ {
		chatID := int64(100 + i)
		cwd := "/repo/common"
		if i%2 == 0 {
			cwd = "/repo/unique"
		}
		err := store.Set(ctx, ProjectBinding{
			Key:         ConversationKey{ChatID: chatID, ThreadID: 0},
			CWD:         cwd,
			ProjectSlug: "-" + cwd,
			Source:      BindingManual,
			CreatedBy:   42,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Limit to 1
	result, err := store.ListByUser(ctx, 42, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("limit=1 expected 1 binding, got %d", len(result))
	}

	// Limit to 10 — dedup should yield 2 unique CWDs
	result, err = store.ListByUser(ctx, 42, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 unique CWDs after dedup, got %d: %v", len(result), result)
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
