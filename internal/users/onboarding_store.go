package users

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// OnboardingState holds a user's conversational onboarding progress.
type OnboardingState struct {
	UserID    int64
	ChatID    int64
	ThreadID  int
	Step      string // "name" | "bio" | "done"
	Name      string
	Language  string
	Bio       string
	FirstMsg  string
	StartedAt time.Time
	UpdatedAt time.Time
}

// OnboardingStore persists onboarding state in SQLite.
type OnboardingStore struct {
	db *sql.DB
}

// NewOnboardingStore creates an OnboardingStore backed by the given SQLite DB.
func NewOnboardingStore(db *sql.DB) *OnboardingStore {
	return &OnboardingStore{db: db}
}

// NewOnboardingStoreFromFile opens or creates a SQLite database at dbPath and returns
// an OnboardingStore with schema ensured.
func NewOnboardingStoreFromFile(dbPath string) (*OnboardingStore, error) {
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open onboarding sqlite store: %w", err)
	}
	store := &OnboardingStore{db: db}
	if err := store.EnsureSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ensure onboarding schema: %w", err)
	}
	return store, nil
}

// Close closes the underlying database connection.
func (s *OnboardingStore) Close() error {
	return s.db.Close()
}

// EnsureSchema creates the user_onboarding table if it doesn't exist.
func (s *OnboardingStore) EnsureSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS user_onboarding (
			user_id     INTEGER PRIMARY KEY,
			chat_id     INTEGER NOT NULL,
			thread_id   INTEGER NOT NULL,
			step        TEXT NOT NULL,
			name        TEXT,
			language    TEXT,
			bio         TEXT,
			first_msg   TEXT,
			started_at  INTEGER NOT NULL,
			updated_at  INTEGER NOT NULL
		)
	`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_onboarding_updated ON user_onboarding(updated_at)`)
	return err
}

// Begin inserts a new onboarding record.
func (s *OnboardingStore) Begin(state *OnboardingState) error {
	_, err := s.db.Exec(`
		INSERT INTO user_onboarding (user_id, chat_id, thread_id, step, name, language, bio, first_msg, started_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, state.UserID, state.ChatID, state.ThreadID, state.Step, state.Name,
		state.Language, state.Bio, state.FirstMsg,
		state.StartedAt.Unix(), state.UpdatedAt.Unix())
	if err != nil {
		return fmt.Errorf("begin onboarding: %w", err)
	}
	return nil
}

// Get retrieves the onboarding state for a user. Returns nil, nil if not found.
func (s *OnboardingStore) Get(userID int64) (*OnboardingState, error) {
	row := s.db.QueryRow(`
		SELECT user_id, chat_id, thread_id, step, name, language, bio, first_msg, started_at, updated_at
		FROM user_onboarding WHERE user_id = ?
	`, userID)
	var (
		state     OnboardingState
		startedAt int64
		updatedAt int64
	)
	if err := row.Scan(&state.UserID, &state.ChatID, &state.ThreadID,
		&state.Step, &state.Name, &state.Language, &state.Bio, &state.FirstMsg,
		&startedAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get onboarding: %w", err)
	}
	state.StartedAt = time.Unix(startedAt, 0)
	state.UpdatedAt = time.Unix(updatedAt, 0)
	return &state, nil
}

// Update persists changes to an existing onboarding record.
func (s *OnboardingStore) Update(state *OnboardingState) error {
	_, err := s.db.Exec(`
		UPDATE user_onboarding SET chat_id=?, thread_id=?, step=?, name=?, language=?, bio=?, first_msg=?, updated_at=?
		WHERE user_id = ?
	`, state.ChatID, state.ThreadID, state.Step, state.Name, state.Language,
		state.Bio, state.FirstMsg, state.UpdatedAt.Unix(), state.UserID)
	if err != nil {
		return fmt.Errorf("update onboarding: %w", err)
	}
	return nil
}

// Delete removes the onboarding state for a user.
func (s *OnboardingStore) Delete(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM user_onboarding WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete onboarding: %w", err)
	}
	return nil
}

// Cleanup removes onboarding rows older than before. Returns count of deleted rows.
func (s *OnboardingStore) Cleanup(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM user_onboarding WHERE updated_at < ?`, before.Unix())
	if err != nil {
		return 0, fmt.Errorf("cleanup onboarding: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
