package users

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStore_SaveAndGet(t *testing.T) {
	root := t.TempDir()
	r := NewResolver(root)
	s := NewStore(r)

	p := &Profile{UserID: 1, Name: "Alice", Language: "pt"}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Get(1)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if got.Name != "Alice" {
		t.Errorf("Name = %q, want %q", got.Name, "Alice")
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	root := t.TempDir()
	r := NewResolver(root)
	s := NewStore(r)

	got, err := s.Get(999)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != nil {
		t.Fatal("Get() should return nil for non-existent user")
	}
}

func TestStore_Exists(t *testing.T) {
	root := t.TempDir()
	r := NewResolver(root)
	s := NewStore(r)

	if s.Exists(1) {
		t.Fatal("Exists() should be false before Save")
	}

	p := &Profile{UserID: 1, Name: "Bob"}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if !s.Exists(1) {
		t.Fatal("Exists() should be true after Save")
	}
}

func TestStore_List(t *testing.T) {
	root := t.TempDir()
	r := NewResolver(root)
	s := NewStore(r)

	// Initially empty
	profiles, err := s.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("expected 0 profiles, got %d", len(profiles))
	}

	// Save two profiles
	for _, id := range []int64{1, 2} {
		if err := s.Save(&Profile{UserID: id, Name: "User"}); err != nil {
			t.Fatalf("Save(%d) error = %v", id, err)
		}
	}

	profiles, err = s.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}

	// Non-numeric dir under users/ should be skipped
	nonNumericDir := filepath.Join(r.UserRoot(0), "..", "not-a-number")
	if err := os.MkdirAll(nonNumericDir, 0o700); err != nil {
		t.Fatal(err)
	}
	profiles, err = s.List()
	if err != nil {
		t.Fatalf("List() error after adding non-numeric dir = %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected still 2 profiles, got %d", len(profiles))
	}
}

func TestStore_Delete(t *testing.T) {
	root := t.TempDir()
	r := NewResolver(root)
	s := NewStore(r)

	p := &Profile{UserID: 1, Name: "Charlie"}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := s.Delete(1); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if s.Exists(1) {
		t.Fatal("Exists() should be false after Delete")
	}
}

func TestStore_List_EmptyDir(t *testing.T) {
	root := t.TempDir()
	r := NewResolver(root)
	s := NewStore(r)

	profiles, err := s.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("expected 0 profiles, got %d", len(profiles))
	}
}
