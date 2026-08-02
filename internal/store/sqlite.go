package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/aeon022/diaryctl/internal/models"
	coreconfig "github.com/aeon022/missionctl-core/config"
	"github.com/aeon022/missionctl-core/syncdir"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS repos (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS entries (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    date       TEXT NOT NULL UNIQUE,
    body       TEXT NOT NULL DEFAULT '',
    generated  INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
`

// Store is the SQLite-backed diary store.
type Store struct {
	db   *sql.DB
	path string
}

// DefaultDBPath returns the default path for the diary database.
func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "diaryctl", "diary.db"), nil
}

// ResolveDBPath returns the database file path, and whether it's a
// user-configured (possibly folder-synced) directory rather than the
// private default — set via the DIARYCTL_DATA_DIR env var, e.g. pointing
// inside iCloud Drive or Dropbox. diaryctl has no config-file layer for
// this one setting, so an env var is the only override, deliberately
// minimal rather than introducing a whole new config file for it.
func ResolveDBPath() (path string, shared bool, err error) {
	if dir := os.Getenv("DIARYCTL_DATA_DIR"); dir != "" {
		resolved, sh := coreconfig.ResolveDir("diaryctl", dir)
		return filepath.Join(resolved, "diary.db"), sh, nil
	}
	p, err := DefaultDBPath()
	return p, false, err
}

// diaryctl opens a fresh *Store per operation rather than holding one open
// for the process's lifetime, and flock(2) isn't reentrant within a
// process — locks reference-counts the real OS-level lock per path so the
// same process's own concurrent/sequential opens don't conflict with
// themselves; only the first open of a path acquires it for real, and only
// the last matching Close() releases it. A conflict is reported only when
// a genuinely different process holds it.
var (
	lockMu sync.Mutex
	locks  = map[string]*lockEntry{}
)

type lockEntry struct {
	lock  *syncdir.Lock
	count int
}

func acquireLock(path string) error {
	lockMu.Lock()
	defer lockMu.Unlock()
	e, ok := locks[path]
	if !ok {
		l, err := syncdir.Acquire(path)
		if err != nil {
			return err
		}
		e = &lockEntry{lock: l}
		locks[path] = e
	}
	e.count++
	return nil
}

func releaseLock(path string) {
	lockMu.Lock()
	defer lockMu.Unlock()
	e, ok := locks[path]
	if !ok {
		return
	}
	e.count--
	if e.count == 0 {
		e.lock.Release()
		delete(locks, path)
	}
}

// Open opens (or creates) the SQLite database at the given path. shared
// must reflect whether path is a user-configured (possibly folder-synced)
// directory rather than the private default — see ResolveDBPath.
func Open(path string, shared bool) (*Store, error) {
	if isPlaceholder, placeholder := syncdir.ICloudPlaceholder(path); isPlaceholder {
		return nil, fmt.Errorf("%s hasn't finished downloading from iCloud yet (found %s) — open Finder and download it, or disable \"Optimize Mac Storage\" for this folder", path, placeholder)
	}

	if err := acquireLock(path); err != nil {
		if errors.Is(err, syncdir.ErrLocked) {
			return nil, fmt.Errorf("diaryctl is already running elsewhere, or a previous session crashed — remove %s.lock if you're sure nothing else is using it", path)
		}
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		releaseLock(path)
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_journal_mode="+syncdir.JournalMode(shared)+"&_foreign_keys=on&_timeout=5000")
	if err != nil {
		releaseLock(path)
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		releaseLock(path)
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	return &Store{db: db, path: path}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	err := s.db.Close()
	releaseLock(s.path)
	return err
}

// --- Repo methods ---

// SaveRepo inserts or replaces a repo record.
func (s *Store) SaveRepo(path, name string) error {
	_, err := s.db.Exec(
		`INSERT INTO repos (path, name) VALUES (?, ?)
         ON CONFLICT(path) DO UPDATE SET name=excluded.name`,
		path, name,
	)
	return err
}

// ListRepos returns all registered repos.
func (s *Store) ListRepos() ([]models.Repo, error) {
	rows, err := s.db.Query(`SELECT id, path, name FROM repos ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []models.Repo
	for rows.Next() {
		var r models.Repo
		if err := rows.Scan(&r.ID, &r.Path, &r.Name); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

// DeleteRepo removes a repo by path.
func (s *Store) DeleteRepo(path string) error {
	_, err := s.db.Exec(`DELETE FROM repos WHERE path = ?`, path)
	return err
}

// --- Entry methods ---

// GetEntry retrieves the entry for a given date (YYYY-MM-DD). Returns nil if not found.
func (s *Store) GetEntry(date time.Time) (*models.Entry, error) {
	key := date.Format("2006-01-02")
	row := s.db.QueryRow(
		`SELECT id, date, body, generated, created_at, updated_at FROM entries WHERE date = ?`,
		key,
	)

	var e models.Entry
	var dateStr, createdStr, updatedStr string
	var generated int
	err := row.Scan(&e.ID, &dateStr, &e.Body, &generated, &createdStr, &updatedStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	e.Date, _ = time.Parse("2006-01-02", dateStr)
	e.Generated = generated != 0
	e.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
	e.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedStr)
	return &e, nil
}

// SaveEntry inserts or updates an entry for the given date.
func (s *Store) SaveEntry(date time.Time, body string, generated bool) error {
	key := date.Format("2006-01-02")
	gen := 0
	if generated {
		gen = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO entries (date, body, generated)
         VALUES (?, ?, ?)
         ON CONFLICT(date) DO UPDATE SET
             body=excluded.body,
             generated=excluded.generated,
             updated_at=datetime('now')`,
		key, body, gen,
	)
	return err
}

// ListEntries returns the most recent N entries.
func (s *Store) ListEntries(limit int) ([]models.Entry, error) {
	rows, err := s.db.Query(
		`SELECT id, date, body, generated, created_at, updated_at
         FROM entries ORDER BY date DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []models.Entry
	for rows.Next() {
		var e models.Entry
		var dateStr, createdStr, updatedStr string
		var generated int
		if err := rows.Scan(&e.ID, &dateStr, &e.Body, &generated, &createdStr, &updatedStr); err != nil {
			return nil, err
		}
		e.Date, _ = time.Parse("2006-01-02", dateStr)
		e.Generated = generated != 0
		e.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		e.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedStr)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// DeleteEntry removes an entry by date.
func (s *Store) DeleteEntry(date time.Time) error {
	key := date.Format("2006-01-02")
	_, err := s.db.Exec(`DELETE FROM entries WHERE date = ?`, key)
	return err
}

// GetStreak returns the current consecutive-day streak with at least one commit entry.
// It counts backwards from today, checking entry existence.
func (s *Store) GetStreak() (int, error) {
	rows, err := s.db.Query(
		`SELECT date FROM entries WHERE body != '' ORDER BY date DESC`,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var dates []time.Time
	for rows.Next() {
		var ds string
		if err := rows.Scan(&ds); err != nil {
			return 0, err
		}
		t, _ := time.Parse("2006-01-02", ds)
		dates = append(dates, t)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	if len(dates) == 0 {
		return 0, nil
	}

	today := time.Now().Truncate(24 * time.Hour)
	streak := 0
	expected := today
	for _, d := range dates {
		d = d.Truncate(24 * time.Hour)
		if d.Equal(expected) || d.Equal(expected.AddDate(0, 0, -1)) {
			streak++
			expected = d.AddDate(0, 0, -1)
		} else {
			break
		}
	}
	return streak, nil
}
