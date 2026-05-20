package users

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Profile holds mutable per-user metadata.
type Profile struct {
	UserID      int64     `json:"user_id"`
	Name        string    `json:"name"`
	Language    string    `json:"language"` // "pt" or "en"
	IsOwner     bool      `json:"is_owner"`
	OnboardedAt time.Time `json:"onboarded_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

// Load reads a Profile from a JSON file. Returns nil, nil if file does not exist.
func Load(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}
	return &p, nil
}

// Save writes a Profile to a JSON file, creating parent directories as needed.
// Uses atomic write (temp file + rename) to avoid partial writes on crash.
func Save(path string, p *Profile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create profile dir: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	data = append(data, '\n')
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write profile: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename profile: %w", err)
	}
	return nil
}
