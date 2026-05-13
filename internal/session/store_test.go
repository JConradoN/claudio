package session

import "testing"

func TestStore_SetGetClear(t *testing.T) {
	s := NewStore()
	if id := s.Get(1, 0); id != "" {
		t.Fatalf("expected empty, got %q", id)
	}
	s.Set(1, 0, "sess-abc")
	if id := s.Get(1, 0); id != "sess-abc" {
		t.Fatalf("expected sess-abc, got %q", id)
	}
	sid, active := s.GetWithState(1, 0)
	if sid != "sess-abc" || !active {
		t.Fatalf("expected sess-abc/active, got %q/%v", sid, active)
	}
	s.Clear(1, 0)
	if id := s.Get(1, 0); id != "" {
		t.Fatalf("expected empty after clear, got %q", id)
	}
}

func TestStore_ThreadIsolation(t *testing.T) {
	s := NewStore()
	s.Set(1, 0, "sess-main")
	s.Set(1, 42, "sess-topic-42")
	s.Set(1, 99, "sess-topic-99")

	// Each thread has its own session
	if id := s.Get(1, 0); id != "sess-main" {
		t.Fatalf("thread 0 = %q, want sess-main", id)
	}
	if id := s.Get(1, 42); id != "sess-topic-42" {
		t.Fatalf("thread 42 = %q, want sess-topic-42", id)
	}
	if id := s.Get(1, 99); id != "sess-topic-99" {
		t.Fatalf("thread 99 = %q, want sess-topic-99", id)
	}

	// Clear only one thread
	s.Clear(1, 42)
	if id := s.Get(1, 42); id != "" {
		t.Fatalf("thread 42 should be empty after clear, got %q", id)
	}
	if id := s.Get(1, 0); id != "sess-main" {
		t.Fatalf("thread 0 should be preserved, got %q", id)
	}

	// ClearAll removes all threads
	s.ClearAll(1)
	if id := s.Get(1, 0); id != "" {
		t.Fatalf("thread 0 should be empty after ClearAll, got %q", id)
	}
	if id := s.Get(1, 99); id != "" {
		t.Fatalf("thread 99 should be empty after ClearAll, got %q", id)
	}
}

func TestStore_DeactivateAll(t *testing.T) {
	s := NewStore()
	s.Set(1, 0, "sess-a")
	s.Set(2, 0, "sess-b")
	s.Set(3, 0, "sess-c")

	// All active before deactivation
	for _, chatID := range []int64{1, 2, 3} {
		if _, active := s.GetWithState(chatID, 0); !active {
			t.Fatalf("chat %d should be active before DeactivateAll", chatID)
		}
	}

	s.DeactivateAll()

	// All inactive after deactivation, but IDs preserved
	for _, chatID := range []int64{1, 2, 3} {
		sid, active := s.GetWithState(chatID, 0)
		if active {
			t.Fatalf("chat %d should be inactive after DeactivateAll", chatID)
		}
		if sid == "" {
			t.Fatalf("chat %d session ID should be preserved after DeactivateAll", chatID)
		}
	}

	// Get still returns the session ID
	if id := s.Get(1, 0); id != "sess-a" {
		t.Fatalf("Get(1, 0) = %q, want %q", id, "sess-a")
	}
}

func TestStore_DeactivateAll_Empty(t *testing.T) {
	s := NewStore()
	s.DeactivateAll() // should not panic
}

func TestStore_Cwd(t *testing.T) {
	s := NewStore()
	s.SetCwd(1, 0, "/home/user")
	if cwd := s.GetCwd(1, 0); cwd != "/home/user" {
		t.Fatalf("expected /home/user, got %q", cwd)
	}
	s.Clear(1, 0)
	if cwd := s.GetCwd(1, 0); cwd != "" {
		t.Fatalf("expected empty after clear, got %q", cwd)
	}
}
