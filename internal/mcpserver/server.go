package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/diaryctl/internal/git"
	"github.com/aeon022/diaryctl/internal/models"
	"github.com/aeon022/diaryctl/internal/store"
	"github.com/aeon022/diaryctl/internal/suite"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Serve starts the MCP server on stdio.
func Serve(s *store.Store) error {
	srv := server.NewMCPServer(
		"diaryctl",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// 1. get_today_stats
	srv.AddTool(
		mcp.NewTool("get_today_stats",
			mcp.WithDescription("Returns git commit stats for today across all registered repos."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleGetTodayStats(ctx, req, s)
		},
	)

	// 2. get_diary_entry
	srv.AddTool(
		mcp.NewTool("get_diary_entry",
			mcp.WithDescription("Returns the diary entry body for a given date (YYYY-MM-DD). Defaults to today."),
			mcp.WithString("date",
				mcp.Description("Date in YYYY-MM-DD format. Defaults to today."),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleGetDiaryEntry(ctx, req, s)
		},
	)

	// 3. write_diary_entry
	srv.AddTool(
		mcp.NewTool("write_diary_entry",
			mcp.WithDescription("Saves or overwrites the diary entry for a given date. Sets the generated flag to true."),
			mcp.WithString("date",
				mcp.Description("Date in YYYY-MM-DD format. Defaults to today."),
			),
			mcp.WithString("body",
				mcp.Description("Markdown content of the diary entry."),
				mcp.Required(),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleWriteDiaryEntry(ctx, req, s)
		},
	)

	// 4. get_coding_stats
	srv.AddTool(
		mcp.NewTool("get_coding_stats",
			mcp.WithDescription("Returns aggregate coding stats for the last N days."),
			mcp.WithNumber("days",
				mcp.Description("Number of days to look back. Defaults to 7."),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleGetCodingStats(ctx, req, s)
		},
	)

	// 5. list_diary_entries
	srv.AddTool(
		mcp.NewTool("list_diary_entries",
			mcp.WithDescription("Returns a list of diary entries with date and preview."),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of entries to return. Defaults to 10."),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleListDiaryEntries(ctx, req, s)
		},
	)

	return server.ServeStdio(srv)
}

// --- Tool handlers ---

func handleGetTodayStats(_ context.Context, _ mcp.CallToolRequest, s *store.Store) (*mcp.CallToolResult, error) {
	repos, err := s.ListRepos()
	if err != nil {
		return toolError(err), nil
	}

	today := time.Now()
	ds, err := git.DayStats(repos, today)
	if err != nil {
		return toolError(err), nil
	}

	type commitJSON struct {
		Hash      string `json:"hash"`
		Message   string `json:"message"`
		Author    string `json:"author"`
		Timestamp string `json:"timestamp"`
		Files     int    `json:"files"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
	}
	type todayStatsResponse struct {
		Date           string       `json:"date"`
		Repos          []string     `json:"repos"`
		TotalCommits   int          `json:"total_commits"`
		TotalFiles     int          `json:"total_files"`
		TotalAdded     int          `json:"total_added"`
		TotalDeleted   int          `json:"total_deleted"`
		ActiveMins     int          `json:"active_mins"`
		Commits        []commitJSON `json:"commits"`
		CompletedTasks int          `json:"completed_tasks"`
		CalendarEvents int          `json:"calendar_events"`
		TimeTrackedSec int64        `json:"time_tracked_seconds"`
		TimeTrackedStr string       `json:"time_tracked_human"`
	}

	var commits []commitJSON
	for _, c := range ds.Commits {
		commits = append(commits, commitJSON{
			Hash:      c.Hash,
			Message:   c.Message,
			Author:    c.Author,
			Timestamp: c.Timestamp.Format(time.RFC3339),
			Files:     c.Files,
			Additions: c.Additions,
			Deletions: c.Deletions,
		})
	}

	tasks, _ := suite.TodayTasks()
	events, _ := suite.TodayEvents()
	timeEntries, _ := suite.TodayTimeEntries()
	totalDur := suite.TotalDuration(timeEntries)

	res := todayStatsResponse{
		Date:           today.Format("2006-01-02"),
		Repos:          ds.Repos,
		TotalCommits:   len(ds.Commits),
		TotalFiles:     ds.TotalFiles,
		TotalAdded:     ds.TotalAdded,
		TotalDeleted:   ds.TotalDeleted,
		ActiveMins:     ds.ActiveMins,
		Commits:        commits,
		CompletedTasks: len(tasks),
		CalendarEvents: len(events),
		TimeTrackedSec: int64(totalDur.Seconds()),
		TimeTrackedStr: formatDuration(totalDur),
	}

	return toolJSON(res)
}

func handleGetDiaryEntry(_ context.Context, req mcp.CallToolRequest, s *store.Store) (*mcp.CallToolResult, error) {
	date := time.Now()
	dateStr := req.GetString("date", "")
	if dateStr != "" {
		var err error
		date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			return toolError(fmt.Errorf("invalid date format, use YYYY-MM-DD")), nil
		}
	}

	entry, err := s.GetEntry(date)
	if err != nil {
		return toolError(err), nil
	}
	if entry == nil {
		return mcp.NewToolResultText(fmt.Sprintf("No entry found for %s", date.Format("2006-01-02"))), nil
	}

	return mcp.NewToolResultText(entry.Body), nil
}

func handleWriteDiaryEntry(_ context.Context, req mcp.CallToolRequest, s *store.Store) (*mcp.CallToolResult, error) {
	body := req.GetString("body", "")
	if body == "" {
		return toolError(fmt.Errorf("body is required")), nil
	}

	date := time.Now()
	dateStr := req.GetString("date", "")
	if dateStr != "" {
		var err error
		date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			return toolError(fmt.Errorf("invalid date format, use YYYY-MM-DD")), nil
		}
	}

	if err := s.SaveEntry(date, body, true); err != nil {
		return toolError(err), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Entry saved for %s", date.Format("2006-01-02"))), nil
}

func handleGetCodingStats(_ context.Context, req mcp.CallToolRequest, s *store.Store) (*mcp.CallToolResult, error) {
	days := int(req.GetFloat("days", 7))
	if days <= 0 {
		days = 7
	}

	repos, err := s.ListRepos()
	if err != nil {
		return toolError(err), nil
	}

	dayStats, err := git.RecentDays(repos, days)
	if err != nil {
		return toolError(err), nil
	}

	streak, _ := s.GetStreak()

	type repoBreakdown struct {
		Name    string `json:"name"`
		Commits int    `json:"commits"`
	}

	totalCommits := 0
	totalFiles := 0
	totalAdded := 0
	totalDeleted := 0
	repoCounts := map[string]int{}
	dowCounts := map[string]int{}

	for _, ds := range dayStats {
		totalCommits += len(ds.Commits)
		totalFiles += ds.TotalFiles
		totalAdded += ds.TotalAdded
		totalDeleted += ds.TotalDeleted
		for _, r := range ds.Repos {
			repoCounts[r] += len(ds.Commits)
		}
		if len(ds.Commits) > 0 {
			dowCounts[ds.Date.Weekday().String()]++
		}
	}

	var breakdown []repoBreakdown
	for name, count := range repoCounts {
		breakdown = append(breakdown, repoBreakdown{Name: name, Commits: count})
	}

	mostActive := ""
	maxDow := 0
	for day, count := range dowCounts {
		if count > maxDow {
			maxDow = count
			mostActive = day
		}
	}

	type result struct {
		Period        int             `json:"period_days"`
		TotalCommits  int             `json:"total_commits"`
		TotalFiles    int             `json:"total_files"`
		TotalAdded    int             `json:"total_added"`
		TotalDeleted  int             `json:"total_deleted"`
		Streak        int             `json:"streak_days"`
		MostActiveDay string          `json:"most_active_day"`
		RepoBreakdown []repoBreakdown `json:"repo_breakdown"`
	}

	res := result{
		Period:        days,
		TotalCommits:  totalCommits,
		TotalFiles:    totalFiles,
		TotalAdded:    totalAdded,
		TotalDeleted:  totalDeleted,
		Streak:        streak,
		MostActiveDay: mostActive,
		RepoBreakdown: breakdown,
	}

	return toolJSON(res)
}

func handleListDiaryEntries(_ context.Context, req mcp.CallToolRequest, s *store.Store) (*mcp.CallToolResult, error) {
	limit := int(req.GetFloat("limit", 10))
	if limit <= 0 {
		limit = 10
	}

	entries, err := s.ListEntries(limit)
	if err != nil {
		return toolError(err), nil
	}

	type entryJSON struct {
		Date      string `json:"date"`
		Preview   string `json:"preview"`
		Generated bool   `json:"generated"`
	}

	var result []entryJSON
	for _, e := range entries {
		preview := e.Body
		if len(preview) > 100 {
			preview = preview[:100] + "…"
		}
		result = append(result, entryJSON{
			Date:      e.Date.Format("2006-01-02"),
			Preview:   preview,
			Generated: e.Generated,
		})
	}

	return toolJSON(result)
}

// --- helpers ---

// formatDuration formats a duration as "Xh Ym" (no seconds).
func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 && m > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

func toolError(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(err.Error())
}

func toolJSON(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return toolError(err), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

// FormatStats formats DayStats as human-readable text.
func FormatStats(ds models.DayStats) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Date:    %s\n", ds.Date.Format("2006-01-02")))
	sb.WriteString(fmt.Sprintf("Commits: %d\n", len(ds.Commits)))
	sb.WriteString(fmt.Sprintf("Files:   %d changed\n", ds.TotalFiles))
	sb.WriteString(fmt.Sprintf("Lines:   +%d / -%d\n", ds.TotalAdded, ds.TotalDeleted))
	if ds.ActiveMins > 0 {
		sb.WriteString(fmt.Sprintf("Active:  ~%d min\n", ds.ActiveMins))
	}
	return sb.String()
}
