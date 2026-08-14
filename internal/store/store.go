// Package store persists document snapshots in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/ncruces/go-sqlite3/driver" // registers the "sqlite3" driver
)

const schema = `
CREATE TABLE IF NOT EXISTS documents (
    id          TEXT PRIMARY KEY,
    readonly_id TEXT NOT NULL UNIQUE,
    text        TEXT NOT NULL,
    language    TEXT NOT NULL DEFAULT '',
    ttl_seconds INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_documents_expires ON documents(expires_at);
CREATE INDEX IF NOT EXISTS idx_documents_updated ON documents(updated_at DESC);
`

// Row is one persisted document snapshot. All timestamps are unix seconds.
type Row struct {
	ID         string
	ReadonlyID string
	Text       string
	Language   string
	TTLSeconds int64
	CreatedAt  int64
	UpdatedAt  int64
	ExpiresAt  int64
}

type Store struct {
	db *sql.DB
}

// Open creates or opens the SQLite database at path.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// A single connection serializes writes, avoiding SQLITE_BUSY between
	// concurrent writers.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: init schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Save inserts or replaces a document snapshot.
func (s *Store) Save(r Row) error {
	_, err := s.db.Exec(`
		INSERT INTO documents (id, readonly_id, text, language, ttl_seconds, created_at, updated_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			text = excluded.text,
			language = excluded.language,
			ttl_seconds = excluded.ttl_seconds,
			updated_at = excluded.updated_at,
			expires_at = excluded.expires_at`,
		r.ID, r.ReadonlyID, r.Text, r.Language, r.TTLSeconds, r.CreatedAt, r.UpdatedAt, r.ExpiresAt)
	if err != nil {
		return fmt.Errorf("store: save %s: %w", r.ID, err)
	}
	return nil
}

// Load returns the row with the given id, or nil when absent.
func (s *Store) Load(id string) (*Row, error) {
	return s.loadWhere("id = ?", id)
}

// LoadByReadonlyID returns the row with the given readonly id, or nil.
func (s *Store) LoadByReadonlyID(roID string) (*Row, error) {
	return s.loadWhere("readonly_id = ?", roID)
}

func (s *Store) loadWhere(cond string, arg any) (*Row, error) {
	var r Row
	err := s.db.QueryRow(
		`SELECT id, readonly_id, text, language, ttl_seconds, created_at, updated_at, expires_at
		 FROM documents WHERE `+cond, arg).
		Scan(&r.ID, &r.ReadonlyID, &r.Text, &r.Language, &r.TTLSeconds, &r.CreatedAt, &r.UpdatedAt, &r.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load: %w", err)
	}
	return &r, nil
}

// Delete removes a document.
func (s *Store) Delete(id string) error {
	if _, err := s.db.Exec(`DELETE FROM documents WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete %s: %w", id, err)
	}
	return nil
}

// DeleteExpired removes every document whose expiry has passed and returns
// how many were removed.
func (s *Store) DeleteExpired(now int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM documents WHERE expires_at < ?`, now)
	if err != nil {
		return 0, fmt.Errorf("store: delete expired: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// List returns rows ordered by most recently updated first.
func (s *Store) List(offset, limit int) ([]Row, error) {
	rows, err := s.db.Query(
		`SELECT id, readonly_id, text, language, ttl_seconds, created_at, updated_at, expires_at
		 FROM documents ORDER BY updated_at DESC, id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: list: %w", err)
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.ID, &r.ReadonlyID, &r.Text, &r.Language, &r.TTLSeconds, &r.CreatedAt, &r.UpdatedAt, &r.ExpiresAt); err != nil {
			return nil, fmt.Errorf("store: list scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Count returns the total number of persisted documents.
func (s *Store) Count() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count: %w", err)
	}
	return n, nil
}
