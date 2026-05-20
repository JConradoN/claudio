package users

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProfile_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")

	now := time.Now().Round(time.Second).UTC()
	original := &Profile{
		UserID:      42,
		Name:        "Alice",
		Language:    "pt",
		IsOwner:     true,
		OnboardedAt: now,
		LastSeenAt:  now,
	}

	if err := Save(path, original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got == nil {
		t.Fatal("Load() returned nil")
	}

	if got.UserID != original.UserID {
		t.Errorf("UserID = %d, want %d", got.UserID, original.UserID)
	}
	if got.Name != original.Name {
		t.Errorf("Name = %q, want %q", got.Name, original.Name)
	}
	if got.Language != original.Language {
		t.Errorf("Language = %q, want %q", got.Language, original.Language)
	}
	if got.IsOwner != original.IsOwner {
		t.Errorf("IsOwner = %v, want %v", got.IsOwner, original.IsOwner)
	}
	if !got.OnboardedAt.Equal(original.OnboardedAt) {
		t.Errorf("OnboardedAt = %v, want %v", got.OnboardedAt, original.OnboardedAt)
	}
	if !got.LastSeenAt.Equal(original.LastSeenAt) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, original.LastSeenAt)
	}
}

func TestProfile_LoadMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if p != nil {
		t.Fatal("Load() should return nil for missing file")
	}
}

func TestProfile_SaveCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "nested", "profile.json")

	p := &Profile{
		UserID:   1,
		Name:     "Test",
		Language: "en",
	}
	if err := Save(path, p); err != nil {
		t.Fatalf("Save() to nested dir error = %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Save() did not create the file")
	}
}
