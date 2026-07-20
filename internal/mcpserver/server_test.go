package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aeon022/diaryctl/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// newTestServer registers the same 5 tools Serve() does, against a temp
// store, without calling Serve() itself (which blocks forever on stdio).
// All handlers are local SQLite + local git log reads (git.DayStats) plus
// read-only reads of sister-tool DBs (internal/suite) — nothing external.
func newTestServer(t *testing.T) *mcpserver.MCPServer {
	t.Helper()
	path := filepath.Join(t.TempDir(), "diaryctl.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	srv := mcpserver.NewMCPServer("diaryctl", "test", mcpserver.WithToolCapabilities(true))

	srv.AddTool(
		mcp.NewTool("get_today_stats", mcp.WithDescription("...")),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleGetTodayStats(ctx, req, s)
		},
	)
	srv.AddTool(
		mcp.NewTool("get_diary_entry", mcp.WithDescription("..."),
			mcp.WithString("date", mcp.Description("..."))),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleGetDiaryEntry(ctx, req, s)
		},
	)
	srv.AddTool(
		mcp.NewTool("write_diary_entry", mcp.WithDescription("..."),
			mcp.WithString("date", mcp.Description("...")),
			mcp.WithString("body", mcp.Description("..."), mcp.Required())),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleWriteDiaryEntry(ctx, req, s)
		},
	)
	srv.AddTool(
		mcp.NewTool("get_coding_stats", mcp.WithDescription("..."),
			mcp.WithNumber("days", mcp.Description("..."))),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleGetCodingStats(ctx, req, s)
		},
	)
	srv.AddTool(
		mcp.NewTool("list_diary_entries", mcp.WithDescription("..."),
			mcp.WithNumber("limit", mcp.Description("..."))),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleListDiaryEntries(ctx, req, s)
		},
	)
	return srv
}

// callTool dispatches a tools/call JSON-RPC request through the server —
// works across mcp-go versions (this repo is on v0.32.0, which lacks the
// newer GetTool() convenience method).
func callTool(t *testing.T, srv *mcpserver.MCPServer, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  string(mcp.MethodToolsCall),
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	msg := srv.HandleMessage(context.Background(), raw)
	resp, ok := msg.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected a JSON-RPC response for %q, got %T: %+v", name, msg, msg)
	}
	res, ok := resp.Result.(mcp.CallToolResult)
	if !ok {
		t.Fatalf("expected mcp.CallToolResult for %q, got %T", name, resp.Result)
	}
	if res.IsError {
		t.Fatalf("handler for %q returned an error result: %+v", name, res.Content)
	}
	return &res
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

func TestGetTodayStatsWithNoRepos(t *testing.T) {
	srv := newTestServer(t)

	res := callTool(t, srv, "get_today_stats", nil)
	text := resultText(t, res)
	if !strings.Contains(text, `"total_commits": 0`) {
		t.Errorf("expected zero commits with no registered repos, got:\n%s", text)
	}
}

func TestWriteAndGetDiaryEntry(t *testing.T) {
	srv := newTestServer(t)

	callTool(t, srv, "write_diary_entry", map[string]any{
		"date": "2026-07-20",
		"body": "Shipped the MCP smoke tests today.",
	})

	res := callTool(t, srv, "get_diary_entry", map[string]any{"date": "2026-07-20"})
	if !strings.Contains(resultText(t, res), "Shipped the MCP smoke tests today.") {
		t.Errorf("expected written entry to be readable, got:\n%s", resultText(t, res))
	}
}

func TestGetDiaryEntryMissingDate(t *testing.T) {
	srv := newTestServer(t)

	res := callTool(t, srv, "get_diary_entry", map[string]any{"date": "2020-01-01"})
	if !strings.Contains(resultText(t, res), "No entry found") {
		t.Errorf("expected 'no entry' message, got:\n%s", resultText(t, res))
	}
}

func TestWriteDiaryEntryRequiresBody(t *testing.T) {
	srv := newTestServer(t)

	req := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": string(mcp.MethodToolsCall),
		"params": map[string]any{"name": "write_diary_entry", "arguments": map[string]any{}},
	}
	raw, _ := json.Marshal(req)
	msg := srv.HandleMessage(context.Background(), raw)
	resp := msg.(mcp.JSONRPCResponse)
	res := resp.Result.(mcp.CallToolResult)
	if !res.IsError {
		t.Fatal("expected an error result when body is missing")
	}
}

func TestListDiaryEntries(t *testing.T) {
	srv := newTestServer(t)

	callTool(t, srv, "write_diary_entry", map[string]any{"date": "2026-07-19", "body": "Entry A"})
	callTool(t, srv, "write_diary_entry", map[string]any{"date": "2026-07-20", "body": "Entry B"})

	res := callTool(t, srv, "list_diary_entries", nil)
	text := resultText(t, res)
	if !strings.Contains(text, "2026-07-19") || !strings.Contains(text, "2026-07-20") {
		t.Errorf("expected both entries listed, got:\n%s", text)
	}
}

func TestGetCodingStatsWithNoRepos(t *testing.T) {
	srv := newTestServer(t)

	res := callTool(t, srv, "get_coding_stats", map[string]any{"days": 7})
	text := resultText(t, res)
	if !strings.Contains(text, `"total_commits": 0`) {
		t.Errorf("expected zero commits with no registered repos, got:\n%s", text)
	}
}
