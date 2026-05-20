package users

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestOnboardingDB(t *testing.T) *OnboardingStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "onboarding.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := NewOnboardingStore(db)
	if err := s.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}
	return s
}

func TestOnboardingStore_BeginAndGet(t *testing.T) {
	s := newTestOnboardingDB(t)

	state := &OnboardingState{
		UserID: 1, ChatID: 100, ThreadID: 0,
		Step: "name", Language: "pt", FirstMsg: "olá",
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.Begin(state); err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	got, err := s.Get(1)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if got.Step != "name" {
		t.Errorf("Step = %q, want %q", got.Step, "name")
	}
	if got.FirstMsg != "olá" {
		t.Errorf("FirstMsg = %q, want %q", got.FirstMsg, "olá")
	}
}

func TestOnboardingStore_Get_NotFound(t *testing.T) {
	s := newTestOnboardingDB(t)

	got, err := s.Get(999)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != nil {
		t.Fatal("Get() should return nil for non-existent user")
	}
}

func TestOnboardingStore_Update(t *testing.T) {
	s := newTestOnboardingDB(t)

	state := &OnboardingState{
		UserID: 1, ChatID: 100, ThreadID: 0,
		Step: "name", Language: "pt", FirstMsg: "olá",
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.Begin(state); err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	state.Step = "bio"
	state.Name = "Alice"
	state.UpdatedAt = time.Now()
	if err := s.Update(state); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := s.Get(1)
	if err != nil {
		t.Fatalf("Get() after update error = %v", err)
	}
	if got.Step != "bio" {
		t.Errorf("Step = %q, want %q", got.Step, "bio")
	}
	if got.Name != "Alice" {
		t.Errorf("Name = %q, want %q", got.Name, "Alice")
	}
}

func TestOnboardingStore_Delete(t *testing.T) {
	s := newTestOnboardingDB(t)

	state := &OnboardingState{
		UserID: 1, ChatID: 100, ThreadID: 0,
		Step: "name", Language: "pt", FirstMsg: "olá",
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.Begin(state); err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	if err := s.Delete(1); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	got, err := s.Get(1)
	if err != nil {
		t.Fatalf("Get() after delete error = %v", err)
	}
	if got != nil {
		t.Fatal("Get() should return nil after Delete")
	}
}

func TestOnboardingStore_Cleanup(t *testing.T) {
	s := newTestOnboardingDB(t)

	now := time.Now()
	old := now.Add(-2 * time.Hour)

	// Insert two records: one old, one recent
	oldState := &OnboardingState{
		UserID: 1, ChatID: 100, ThreadID: 0,
		Step: "name", Language: "pt", FirstMsg: "old",
		StartedAt: old, UpdatedAt: old,
	}
	if err := s.Begin(oldState); err != nil {
		t.Fatalf("Begin(old) error = %v", err)
	}

	recentState := &OnboardingState{
		UserID: 2, ChatID: 200, ThreadID: 0,
		Step: "name", Language: "en", FirstMsg: "recent",
		StartedAt: now, UpdatedAt: now,
	}
	if err := s.Begin(recentState); err != nil {
		t.Fatalf("Begin(recent) error = %v", err)
	}

	n, err := s.Cleanup(now.Add(-1 * time.Hour))
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if n != 1 {
		t.Errorf("Cleanup() deleted %d rows, want 1", n)
	}

	// Old record should be gone
	got, err := s.Get(1)
	if err != nil {
		t.Fatalf("Get(old) error = %v", err)
	}
	if got != nil {
		t.Fatal("Get(old) should return nil after cleanup")
	}

	// Recent record should survive
	got, err = s.Get(2)
	if err != nil {
		t.Fatalf("Get(recent) error = %v", err)
	}
	if got == nil {
		t.Fatal("Get(recent) should exist after cleanup")
	}
}

// TestOnboardingStore_EnsureSchema_Idempotent verifies running EnsureSchema twice is safe.
func TestOnboardingStore_EnsureSchema_Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := NewOnboardingStore(db)

	if err := s.EnsureSchema(); err != nil {
		t.Fatalf("first EnsureSchema() error = %v", err)
	}
	if err := s.EnsureSchema(); err != nil {
		t.Fatalf("second EnsureSchema() error = %v", err)
	}
}

// TestOnboardingStore_Begin_Duplicate tests that creating a second state for the same user fails.
func TestOnboardingStore_Begin_Duplicate(t *testing.T) {
	s := newTestOnboardingDB(t)

	state := &OnboardingState{
		UserID: 1, ChatID: 100, ThreadID: 0,
		Step: "name", Language: "pt", FirstMsg: "original",
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.Begin(state); err != nil {
		t.Fatalf("first Begin() error = %v", err)
	}

	// Second Begin with same user_id should fail (PRIMARY KEY conflict)
	state2 := &OnboardingState{
		UserID: 1, ChatID: 200, ThreadID: 1,
		Step: "bio", Language: "en", FirstMsg: "second",
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.Begin(state2); err == nil {
		t.Fatal("expected error on duplicate Begin, got nil")
	}

	// Original state should be preserved
	got, err := s.Get(1)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.FirstMsg != "original" {
		t.Errorf("FirstMsg = %q, want %q", got.FirstMsg, "original")
	}
}
