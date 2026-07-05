package models

import "time"

// Repo represents a registered git repository.
type Repo struct {
	ID   int64
	Path string // absolute path to git repo
	Name string // display name
}

// CommitStat holds statistics for a single commit.
type CommitStat struct {
	Hash      string
	Message   string
	Author    string
	Timestamp time.Time
	Files     int
	Additions int
	Deletions int
}

// DayStats aggregates commit activity for a single day across repos.
type DayStats struct {
	Date         time.Time
	Repos        []string
	Commits      []CommitStat
	TotalFiles   int
	TotalAdded   int
	TotalDeleted int
	ActiveMins   int // estimated from commit spread
	Streak       int // consecutive days with commits
}

// Entry is a diary entry for a given day.
type Entry struct {
	ID        int64
	Date      time.Time
	Body      string    // markdown
	Generated bool      // true if AI-generated via MCP
	CreatedAt time.Time
	UpdatedAt time.Time
}
