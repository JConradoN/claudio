package users

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

// Store loads and persists profiles.
type Store struct {
	resolver *Resolver
}

// NewStore creates a Store backed by the given Resolver.
func NewStore(resolver *Resolver) *Store {
	return &Store{resolver: resolver}
}

// Get loads a profile by user ID. Returns nil, nil if not found.
func (s *Store) Get(userID int64) (*Profile, error) {
	return Load(s.resolver.ProfilePath(userID))
}

// Save persists a profile, creating user directories as needed.
func (s *Store) Save(p *Profile) error {
	if err := s.resolver.EnsureUserDir(p.UserID); err != nil {
		return fmt.Errorf("ensure user dir: %w", err)
	}
	return Save(s.resolver.ProfilePath(p.UserID), p)
}

// Resolver returns the underlying path resolver.
func (s *Store) Resolver() *Resolver {
	return s.resolver
}

// Exists checks whether a profile exists for the user.
func (s *Store) Exists(userID int64) bool {
	_, err := os.Stat(s.resolver.ProfilePath(userID))
	return err == nil
}

// List returns all valid profiles in the users directory.
func (s *Store) List() ([]*Profile, error) {
	usersDir := filepath.Join(s.resolver.root, "users")
	entries, err := os.ReadDir(usersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var profiles []*Profile
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		uid, err := strconv.ParseInt(entry.Name(), 10, 64)
		if err != nil {
			log.Printf("users: skipping non-numeric dir %q in users/", entry.Name())
			continue
		}
		p, err := s.Get(uid)
		if err != nil || p == nil {
			log.Printf("users: skipping user %d: load error=%v exists=%v", uid, err, p != nil)
			continue
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// Delete removes a user's entire directory.
func (s *Store) Delete(userID int64) error {
	return os.RemoveAll(s.resolver.UserRoot(userID))
}
