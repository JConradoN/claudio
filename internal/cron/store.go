package cron

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type SQLiteCronStore struct {
	db *sql.DB
}

func NewSQLiteCronStore(dbPath string) (*SQLiteCronStore, error) {
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_foreign_keys=ON"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open cron sqlite store: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &SQLiteCronStore{db: db}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

// WithTx runs fn inside a database transaction.
func (s *SQLiteCronStore) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *SQLiteCronStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
