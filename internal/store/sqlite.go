package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aeon022/diaryctl/internal/models"
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
	db *sql.DB
}

// DefaultDBPath returns the default path for the diary database.
func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "diaryctl", "diary.db"), nil
}

// Open opens (or creates) the SQLite database at the given path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
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
