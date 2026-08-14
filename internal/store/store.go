// Package store is the persistence layer for app-owned state only (accounts,
// sessions, audit). Backend: SQLite via modernc.org/sqlite — PURE GO, no cgo —
// which keeps the static binary and any future Yocto recipe trivial. The host
// stays authoritative for device state; this never duplicates host config.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database under dataDir and applies the
// schema. Pragmas: WAL for concurrency, busy_timeout to avoid transient locks,
// foreign_keys for cascade deletes.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	dsn := "file:" + filepath.Join(dataDir, "gateway.db") +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite is single-writer; cap connections to keep it simple and safe.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  username      TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  created_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS audit (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  ts       INTEGER NOT NULL,
  username TEXT,
  action   TEXT NOT NULL,
  detail   TEXT,
  ip       TEXT
);`
	_, err := s.db.Exec(schema)
	return err
}

// ---- Users ----

// User is an application account.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// UserCount returns the number of accounts (0 ⇒ first-boot setup needed).
func (s *Store) UserCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser inserts a new account.
func (s *Store) CreateUser(username, passwordHash string) (User, error) {
	now := time.Now()
	res, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)`,
		username, passwordHash, now.Unix())
	if err != nil {
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return User{ID: id, Username: username, PasswordHash: passwordHash, CreatedAt: now}, nil
}

// UserByUsername looks up an account. Found is false when absent.
func (s *Store) UserByUsername(username string) (user User, found bool, err error) {
	var created int64
	row := s.db.QueryRow(
		`SELECT id, username, password_hash, created_at FROM users WHERE username = ?`, username)
	err = row.Scan(&user.ID, &user.Username, &user.PasswordHash, &created)
	if err == sql.ErrNoRows {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	user.CreatedAt = time.Unix(created, 0)
	return user, true, nil
}

// ---- Sessions ----

// CreateSession stores a session row.
func (s *Store) CreateSession(id string, userID int64, expiresAt time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		id, userID, time.Now().Unix(), expiresAt.Unix())
	return err
}

// SessionUser returns the user for a non-expired session. ok is false when the
// session is missing or expired (expired rows are deleted lazily).
func (s *Store) SessionUser(sessionID string) (user User, ok bool, err error) {
	var expires, created int64
	row := s.db.QueryRow(`
SELECT u.id, u.username, u.password_hash, u.created_at, s.expires_at
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.id = ?`, sessionID)
	err = row.Scan(&user.ID, &user.Username, &user.PasswordHash, &created, &expires)
	if err == sql.ErrNoRows {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	if time.Now().Unix() >= expires {
		_ = s.DeleteSession(sessionID)
		return User{}, false, nil
	}
	user.CreatedAt = time.Unix(created, 0)
	return user, true, nil
}

// DeleteSession removes a session (logout).
func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// ---- Audit ----

// AddAudit appends an audit record for a state-changing or security event.
func (s *Store) AddAudit(username, action, detail, ip string) error {
	_, err := s.db.Exec(
		`INSERT INTO audit (ts, username, action, detail, ip) VALUES (?, ?, ?, ?, ?)`,
		time.Now().Unix(), username, action, detail, ip)
	return err
}
