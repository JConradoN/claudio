package runlog

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store backed by a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens or creates the runlog database at dbPath.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// Create the file with restrictive permissions before sql.Open creates it,
	// to prevent world-readable database files (C3 in security review).
	// sql.Open will open the existing file without changing permissions.
	f, err := os.OpenFile(dbPath, os.O_RDONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("create runlog db file: %w", err)
	}
	_ = f.Close()

	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open runlog sqlite store: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &SQLiteStore{db: db}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Ensure existing runlog.db*, runlog.db-wal, runlog.db-shm have
	// owner-only permissions to prevent credential leakage from
	// persisted run data.
	chmod0600 := func(path string) {
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm()&077 != 0 {
			if chmodErr := os.Chmod(path, 0600); chmodErr != nil {
				log.Printf("Warning: failed to chmod runlog file %s: %v", path, chmodErr)
			}
		}
	}
	chmod0600(dbPath)
	chmod0600(dbPath + "-wal")
	chmod0600(dbPath + "-shm")

	return store, nil
}

func (s *SQLiteStore) initialize() error {
	query := `
	CREATE TABLE IF NOT EXISTS run_journal (
		run_id TEXT PRIMARY KEY,
		chat_id INTEGER NOT NULL,
		thread_id INTEGER NOT NULL,
		request_id TEXT NOT NULL,
		session_id TEXT DEFAULT '',
		cwd TEXT DEFAULT '',
		prompt TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'running',
		checkpoint TEXT DEFAULT '',
		tool_summary TEXT DEFAULT '',
		error TEXT DEFAULT '',
		started_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		completed_at INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_run_journal_chat_thread
	ON run_journal(chat_id, thread_id, started_at DESC);
	`
	if _, err := s.db.Exec(query); err != nil {
		return fmt.Errorf("initialize runlog schema: %w", err)
	}
	return nil
}

// Start inserts a new run record with status=running.
func (s *SQLiteStore) Start(ctx context.Context, record RunRecord) error {
	now := unix(time.Now())
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO run_journal
			(run_id, chat_id, thread_id, request_id, session_id, cwd, prompt,
			 status, checkpoint, tool_summary, error,
			 started_at, updated_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.RunID, record.ChatID, record.ThreadID,
		record.RequestID, record.SessionID, record.CWD, record.Prompt,
		RunRunning, record.Checkpoint, record.ToolSummary, record.Error,
		now, now, 0)
	if err != nil {
		return fmt.Errorf("runlog start %s: %w", record.RunID, err)
	}
	return nil
}

// Update applies partial updates to an existing run.
func (s *SQLiteStore) Update(ctx context.Context, update RunUpdate) error {
	now := unix(time.Now())

	// Build SET clause from non-nil fields.
	sets := "updated_at = ?"
	args := []any{now}

	if update.SessionID != nil {
		sets += ", session_id = ?"
		args = append(args, *update.SessionID)
	}
	if update.Status != nil {
		sets += ", status = ?"
		args = append(args, string(*update.Status))
	}
	if update.Checkpoint != nil {
		sets += ", checkpoint = ?"
		args = append(args, *update.Checkpoint)
	}
	if update.ToolSummary != nil {
		sets += ", tool_summary = ?"
		args = append(args, *update.ToolSummary)
	}
	if update.Error != nil {
		sets += ", error = ?"
		args = append(args, *update.Error)
	}
	if update.CompletedAt != nil {
		sets += ", completed_at = ?"
		args = append(args, unix(*update.CompletedAt))
	}

	args = append(args, update.RunID)
	q := fmt.Sprintf("UPDATE run_journal SET %s WHERE run_id = ?", sets)
	_, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("runlog update %s: %w", update.RunID, err)
	}
	return nil
}

// Complete marks a run with a terminal status and optional checkpoint/error.
func (s *SQLiteStore) Complete(ctx context.Context, runID string, status RunStatus, checkpoint, errMsg string) error {
	now := unix(time.Now())
	_, err := s.db.ExecContext(ctx, `
		UPDATE run_journal
		SET status = ?, checkpoint = ?, error = ?,
		    updated_at = ?, completed_at = ?
		WHERE run_id = ?`,
		string(status), checkpoint, errMsg, now, now, runID)
	if err != nil {
		return fmt.Errorf("runlog complete %s: %w", runID, err)
	}
	return nil
}

// Latest returns the most recent run for a chat/thread, ordered by started_at.
func (s *SQLiteStore) Latest(ctx context.Context, chatID int64, threadID int) (*RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT run_id, chat_id, thread_id, request_id, session_id, cwd, prompt,
		       status, checkpoint, tool_summary, error,
		       started_at, updated_at, completed_at
		FROM run_journal
		WHERE chat_id = ? AND thread_id = ?
		ORDER BY started_at DESC, rowid DESC
		LIMIT 1`, chatID, threadID)

	rec, err := scanRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("runlog latest chat=%d thread=%d: %w", chatID, threadID, err)
	}
	return rec, nil
}

// Close releases the database connection.
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

type recordScanner interface {
	Scan(dest ...any) error
}

func scanRecord(row recordScanner) (*RunRecord, error) {
	var r RunRecord
	var status string
	var startedAt, updatedAt, completedAt int64
	err := row.Scan(&r.RunID, &r.ChatID, &r.ThreadID, &r.RequestID,
		&r.SessionID, &r.CWD, &r.Prompt,
		&status, &r.Checkpoint, &r.ToolSummary, &r.Error,
		&startedAt, &updatedAt, &completedAt)
	if err != nil {
		return nil, err
	}
	r.Status = RunStatus(status)
	r.StartedAt = fromUnix(startedAt)
	r.UpdatedAt = fromUnix(updatedAt)
	r.CompletedAt = fromUnix(completedAt)
	return &r, nil
}

func unix(t time.Time) int64 { return t.Unix() }

func fromUnix(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}
