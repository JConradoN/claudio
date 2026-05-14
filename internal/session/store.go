package session

import (
	"fmt"
	"sync"
	"time"
)

// SessionKey uniquely identifies a session within a chat thread.
// For private chats and non-forum groups, ThreadID is 0.
type SessionKey struct {
	ChatID   int64
	ThreadID int // 0 for non-forum chats
}

func (k SessionKey) String() string {
	return fmt.Sprintf("%d:%d", k.ChatID, k.ThreadID)
}

// Store manages session IDs and working directories per chat thread.
type Store struct {
	mu       sync.RWMutex
	sessions map[SessionKey]*entry
	cwds     map[SessionKey]string
}

type entry struct {
	sessionID string
	active    bool
	lastSeen  time.Time
}

// NewStore creates a new session store.
func NewStore() *Store {
	return &Store{
		sessions: make(map[SessionKey]*entry),
		cwds:     make(map[SessionKey]string),
	}
}

// SessionKeyFor returns a SessionKey from chatID and optional threadID.
func SessionKeyFor(chatID int64, threadID int) SessionKey {
	return SessionKey{ChatID: chatID, ThreadID: threadID}
}

func (s *Store) Get(chatID int64, threadID int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := SessionKeyFor(chatID, threadID)
	e := s.sessions[key]
	if e == nil {
		return ""
	}
	return e.sessionID
}

func (s *Store) GetWithState(chatID int64, threadID int) (sessionID string, active bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := SessionKeyFor(chatID, threadID)
	e := s.sessions[key]
	if e == nil {
		return "", false
	}
	return e.sessionID, e.active
}

func (s *Store) Set(chatID int64, threadID int, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKeyFor(chatID, threadID)
	s.sessions[key] = &entry{sessionID: sessionID, active: true, lastSeen: time.Now()}
}

// Clear removes session and cwd for a specific chat thread.
func (s *Store) Clear(chatID int64, threadID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKeyFor(chatID, threadID)
	delete(s.sessions, key)
	delete(s.cwds, key)
}

// ClearAll removes all sessions and cwds for a chat (all threads).
func (s *Store) ClearAll(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.sessions {
		if key.ChatID == chatID {
			delete(s.sessions, key)
			delete(s.cwds, key)
		}
	}
}

// DeactivateAll marks all sessions as inactive (cold). Used when the bridge
// process dies — sessions keep their IDs for resume, but Continue must not be
// used since the process that held them is gone.
func (s *Store) DeactivateAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.sessions {
		e.active = false
	}
}

// GC removes sessions and cwds that have not been seen since maxAge ago.
func (s *Store) GC(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for key, e := range s.sessions {
		if e.lastSeen.Before(cutoff) {
			delete(s.sessions, key)
			delete(s.cwds, key)
		}
	}
}

func (s *Store) GetCwd(chatID int64, threadID int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Try thread-specific cwd first
	key := SessionKeyFor(chatID, threadID)
	if cwd, ok := s.cwds[key]; ok {
		return cwd
	}
	// Fall back to general topic (thread=0) cwd
	if threadID != 0 {
		generalKey := SessionKeyFor(chatID, 0)
		if cwd, ok := s.cwds[generalKey]; ok {
			return cwd
		}
	}
	return ""
}

func (s *Store) SetCwd(chatID int64, threadID int, cwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKeyFor(chatID, threadID)
	s.cwds[key] = cwd
}
