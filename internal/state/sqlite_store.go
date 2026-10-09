package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore is a durable local StateStore implementation. Values remain
// opaque to the backend so sessions, execution records, and future state
// types can share the same storage contract.
type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("sqlite state path cannot be empty")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite state directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite state database: %w", err)
	}
	// A single connection avoids SQLITE_BUSY surprises in this embedded store;
	// WAL still allows readers while a write is being committed.
	db.SetMaxOpenConns(1)
	for _, statement := range []string{"PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = WAL", `
		CREATE TABLE IF NOT EXISTS aos_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			expires_at INTEGER NULL
		)`} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("initialize sqlite state database: %w", err)
		}
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM aos_state WHERE key = ? AND (expires_at IS NULL OR expires_at > ?)`, key, time.Now().UnixMilli()).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *SQLiteStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	var expires any
	if ttl > 0 {
		expires = time.Now().Add(ttl).UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO aos_state(key,value,expires_at) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, expires_at=excluded.expires_at`, key, value, expires)
	return err
}

// SetPersistent stores a record without a TTL. In particular, executions
// paused on external callbacks must survive process restarts and session TTLs.
func (s *SQLiteStore) SetPersistent(ctx context.Context, key, value string) error {
	return s.Set(ctx, key, value, 0)
}

func (s *SQLiteStore) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM aos_state WHERE key = ?`, key)
	return err
}

func (s *SQLiteStore) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key FROM aos_state WHERE substr(key,1,?) = ? AND (expires_at IS NULL OR expires_at > ?) ORDER BY key`, len(prefix), prefix, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *SQLiteStore) Close() error { return s.db.Close() }
