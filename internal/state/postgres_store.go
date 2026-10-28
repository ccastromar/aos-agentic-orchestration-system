package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresStore is the production StateStore: the same opaque key/value
// contract as SQLiteStore, in a PostgreSQL database that several AOS
// instances can share and that is backed up like any other database.
type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(url string) (*PostgresStore, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("postgres state URL cannot be empty")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres state database: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to postgres state database: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS aos_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			expires_at BIGINT NULL
		)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize postgres state database: %w", err)
	}
	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM aos_state WHERE key = $1 AND (expires_at IS NULL OR expires_at > $2)`, key, time.Now().UnixMilli()).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *PostgresStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	var expires any
	if ttl > 0 {
		expires = time.Now().Add(ttl).UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO aos_state(key,value,expires_at) VALUES($1,$2,$3)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, expires_at=excluded.expires_at`, key, value, expires)
	return err
}

// SetPersistent stores a record without a TTL. In particular, executions
// paused on external callbacks must survive process restarts and session TTLs.
func (s *PostgresStore) SetPersistent(ctx context.Context, key, value string) error {
	return s.Set(ctx, key, value, 0)
}

func (s *PostgresStore) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM aos_state WHERE key = $1`, key)
	return err
}

func (s *PostgresStore) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key FROM aos_state WHERE starts_with(key, $1) AND (expires_at IS NULL OR expires_at > $2) ORDER BY key`, prefix, time.Now().UnixMilli())
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

func (s *PostgresStore) Close() error { return s.db.Close() }
