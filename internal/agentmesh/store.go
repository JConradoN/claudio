package agentmesh

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const defaultDBPath = ".agent-mesh/state.db"

// Store wraps the agent-mesh SQLite database.
type Store struct {
	db *sql.DB
}

// DefaultPath returns ~/.agent-mesh/state.db.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, defaultDBPath)
}

// Open opens (or creates) the agent-mesh SQLite database.
func Open(dbPath string) (*Store, error) {
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open agent-mesh db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	s := &Store{db: db}
	if err := s.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) ensureSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS shared_memory (
			key        TEXT PRIMARY KEY,
			value      TEXT NOT NULL,
			agent      TEXT NOT NULL,
			source     TEXT,
			updated_at TEXT DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS tasks (
			id         TEXT PRIMARY KEY,
			type       TEXT NOT NULL,
			agent      TEXT NOT NULL,
			status     TEXT NOT NULL DEFAULT 'pending',
			payload    TEXT,
			result     TEXT,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS audit_log (
			id      INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT,
			agent   TEXT NOT NULL,
			event   TEXT NOT NULL,
			data    TEXT,
			ts      TEXT DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_audit_ts   ON audit_log(ts DESC);
		CREATE INDEX IF NOT EXISTS idx_tasks_stat ON tasks(status, agent);
	`)
	if err != nil {
		return fmt.Errorf("agent-mesh schema: %w", err)
	}
	return nil
}

// LoadContext returns a short text summary of shared_memory for prompt injection.
// prefix filters keys (e.g. "infra", "project"); empty = all. Limit: max entries.
func (s *Store) LoadContext(prefix string, limit int) (string, error) {
	query := `SELECT key, value, agent, updated_at FROM shared_memory`
	args := []any{}
	if prefix != "" {
		query += ` WHERE key LIKE ?`
		args = append(args, prefix+"%")
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return "", fmt.Errorf("load context: %w", err)
	}
	defer rows.Close()

	var buf strings.Builder
	for rows.Next() {
		var key, value, agent, updatedAt string
		if err := rows.Scan(&key, &value, &agent, &updatedAt); err != nil {
			continue
		}
		// Truncate long values to keep prompt size reasonable
		if len(value) > 300 {
			value = value[:300] + "…"
		}
		fmt.Fprintf(&buf, "### %s [%s, %s]\n%s\n\n", key, agent, updatedAt, value)
	}
	return buf.String(), rows.Err()
}

// CreateTask inserts a new task record and an audit event.
func (s *Store) CreateTask(taskID, agent, taskType, payload string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin task tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		INSERT INTO tasks (id, agent, type, status, payload, created_at, updated_at)
		VALUES (?, ?, ?, 'pending', ?, datetime('now'), datetime('now'))
	`, taskID, agent, taskType, payload)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}

	_, err = tx.Exec(`
		INSERT INTO audit_log (task_id, agent, event, data, ts)
		VALUES (?, 'aurelia', 'task_created', ?, datetime('now'))
	`, taskID, fmt.Sprintf(`{"agent":%q,"type":%q}`, agent, taskType))
	if err != nil {
		return fmt.Errorf("insert audit task_created: %w", err)
	}

	return tx.Commit()
}

// MarkRunning updates a task to running status.
func (s *Store) MarkRunning(taskID string) error {
	_, err := s.db.Exec(`
		UPDATE tasks SET status='running', updated_at=datetime('now') WHERE id=?
	`, taskID)
	if err != nil {
		return fmt.Errorf("mark running: %w", err)
	}
	_, _ = s.db.Exec(`
		INSERT INTO audit_log (task_id, agent, event, ts)
		VALUES (?, 'aurelia', 'task_started', datetime('now'))
	`, taskID)
	return nil
}

// FinishTask updates task status to done/failed and records the result.
func (s *Store) FinishTask(taskID, agent, status, result string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin finish tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		UPDATE tasks SET status=?, result=?, updated_at=datetime('now') WHERE id=?
	`, status, result, taskID)
	if err != nil {
		return fmt.Errorf("finish task: %w", err)
	}

	snippet := result
	if len(snippet) > 200 {
		snippet = snippet[:200] + "…"
	}
	eventData := fmt.Sprintf(`{"status":%q,"result_snippet":%q}`, status, snippet)
	_, err = tx.Exec(`
		INSERT INTO audit_log (task_id, agent, event, data, ts)
		VALUES (?, ?, ?, ?, datetime('now'))
	`, taskID, agent, "task_"+status, eventData)
	if err != nil {
		return fmt.Errorf("audit finish: %w", err)
	}

	return tx.Commit()
}

// RecentTasksSummary returns a brief text summary of the last N tasks.
func (s *Store) RecentTasksSummary(limit int) (string, error) {
	rows, err := s.db.Query(`
		SELECT agent, type, status, substr(result, 1, 120), updated_at
		FROM tasks
		ORDER BY updated_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return "", fmt.Errorf("recent tasks: %w", err)
	}
	defer rows.Close()

	var buf strings.Builder
	count := 0
	for rows.Next() {
		var agent, taskType, status string
		var result sql.NullString
		var updatedAt string
		if err := rows.Scan(&agent, &taskType, &status, &result, &updatedAt); err != nil {
			continue
		}
		// Parse updatedAt to a human-readable relative time
		ts, _ := time.Parse("2006-01-02T15:04:05Z", updatedAt)
		if ts.IsZero() {
			ts, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
		}
		fmt.Fprintf(&buf, "- [%s] %s/%s → **%s**", updatedAt, agent, taskType, status)
		if result.Valid && result.String != "" {
			fmt.Fprintf(&buf, "\n  `%s`", result.String)
		}
		buf.WriteString("\n")
		count++
	}
	if count == 0 {
		return "(nenhuma task registrada)", nil
	}
	return buf.String(), rows.Err()
}

// SharedMemorySummary returns a brief listing of all shared_memory keys.
func (s *Store) SharedMemorySummary() (string, error) {
	rows, err := s.db.Query(`
		SELECT key, agent, updated_at FROM shared_memory ORDER BY updated_at DESC
	`)
	if err != nil {
		return "", fmt.Errorf("memory summary: %w", err)
	}
	defer rows.Close()

	var buf strings.Builder
	for rows.Next() {
		var key, agent, updatedAt string
		if err := rows.Scan(&key, &agent, &updatedAt); err != nil {
			continue
		}
		fmt.Fprintf(&buf, "- `%s` [%s, %s]\n", key, agent, updatedAt)
	}
	if buf.Len() == 0 {
		return "(memória vazia)", nil
	}
	return buf.String(), rows.Err()
}
