# diaryctl

A developer diary powered by git history. Part of the missionctl suite.

`diaryctl` reads your git commit history, generates structured diary entries with an AI-ready template, and provides an MCP server so Claude can write narrative reflections directly into your diary.

---

## Quick Start

```bash
# Install
bash setup.sh

# Register a git repo
diaryctl init ~/code/myproject --name "My Project"

# Generate today's entry
diaryctl today

# Open today's entry in your editor
diaryctl today --open

# Open the TUI
diaryctl
```

### MCP Configuration (Claude Desktop / claude.json)

```json
{
  "mcpServers": {
    "diaryctl": {
      "command": "diaryctl",
      "args": ["mcp"]
    }
  }
}
```

---

## Cheatsheet

### CLI

| Command | Description |
|---|---|
| `diaryctl` | Open TUI |
| `diaryctl init [path] [--name NAME]` | Register a git repo |
| `diaryctl repos` | List registered repos |
| `diaryctl today [--open] [--json]` | Generate/show today's entry |
| `diaryctl list [--limit 30] [--json]` | List past entries |
| `diaryctl show [DATE]` | Show a specific entry (YYYY-MM-DD) |
| `diaryctl stats [--days 30]` | Show productivity stats |
| `diaryctl mcp` | Start MCP server on stdio |

### TUI Keys

| Key | Action |
|---|---|
| `j` / `k` | Navigate entries |
| `enter` | Open entry detail |
| `n` | Generate today's entry |
| `e` | Edit selected entry in $EDITOR |
| `r` | Manage repos |
| `v` | Stats view |
| `/` | Search entries |
| `q` | Quit |

**Detail view:**

| Key | Action |
|---|---|
| `j` / `k` | Scroll |
| `e` | Edit in $EDITOR |
| `esc` | Back to list |

---

## CLI Reference

### `diaryctl init [PATH] [--name NAME]`

Register a git repository. PATH defaults to the current directory. Name defaults to the directory basename.

```
$ diaryctl init ~/code/myproject --name "My Project"
✓ Registered: My Project (/Users/you/code/myproject)
```

### `diaryctl repos`

List all registered repositories.

### `diaryctl today [--open] [--json]`

Generate today's diary entry from git history. If an entry already exists, print it.

- `--open`: Open in `$EDITOR` after generating.
- `--json`: Output as JSON.

The entry template includes:
- Stats (commits, files changed, lines, active window, streak)
- Commit list grouped by repo
- `<!-- AI: ... -->` comment prompting Claude to write the narrative
- Reflection and Tomorrow sections

### `diaryctl list [--limit 30] [--json]`

List past entries (date, first line preview, has_content, source).

### `diaryctl show [DATE]`

Show a specific diary entry. DATE defaults to today (format: YYYY-MM-DD).

### `diaryctl stats [--days 30]`

Show productivity stats:
- Streak (consecutive days with commits)
- Total commits / files / lines in the period
- Most active day of week and hour
- Per-repo breakdown
- Daily activity bar chart for last 14 days

### `diaryctl mcp`

Start the MCP server on stdio. Attach via Claude Desktop or any MCP client.

---

## TUI Guide

Run `diaryctl` without arguments to open the TUI.

**List view** (default):
- Left panel: 30-day commit heatmap (block character grid, colored by commit density)
- Right panel: entry list with date and preview
- Status bar: current streak in amber, entry count

**Detail view** (press `enter`):
- Full entry rendered as plain text
- Scroll with `j`/`k`
- Edit with `e` (opens `$EDITOR`, saves on close)
- Back with `esc`

**Repos view** (press `r`):
- List registered repos
- Delete with `d`

---

## MCP Tools

| Tool | Parameters | Description |
|---|---|---|
| `get_today_stats` | — | Git stats for today across all repos |
| `get_diary_entry` | `date` (YYYY-MM-DD, optional) | Read diary entry for a date |
| `write_diary_entry` | `date` (optional), `body` (required) | Save/overwrite diary entry (marks as AI-generated) |
| `get_coding_stats` | `days` (default 7) | Aggregate stats for last N days |
| `list_diary_entries` | `limit` (default 10) | List entries with date and 100-char preview |

---

## AI Workflow Examples

With diaryctl connected as an MCP server, you can ask Claude:

**Narrative diary entry:**
> "Read my today stats and write a developer diary entry for today. Fill in the Context section of my diary."

**Weekly summary:**
> "What did I work on this week? Use get_coding_stats for the last 7 days and get_diary_entry for each day."

**Reflection:**
> "Based on my last 30 days of coding stats, what are my most productive patterns? When do I code best?"

**Auto-write:**
> "Get today's stats, write a narrative diary entry in my voice, and save it with write_diary_entry."

---

## Architecture

```
diaryctl/
├── main.go                        Entry point
├── cmd/
│   ├── root.go                    Root cobra command (opens TUI by default)
│   ├── init.go                    Register a git repo
│   ├── repos.go                   List registered repos
│   ├── today.go                   Generate today's entry
│   ├── list.go                    List past entries
│   ├── show.go                    Show a specific entry
│   ├── stats.go                   Productivity stats
│   └── mcp.go                     Start MCP server
├── internal/
│   ├── diary/
│   │   └── builder.go             Shared entry body builder (git → markdown)
│   ├── models/
│   │   └── models.go              Entry, Repo, CommitStat, DayStats types
│   ├── store/
│   │   └── sqlite.go              SQLite store (WAL mode, ~/.local/share/diaryctl/)
│   ├── git/
│   │   └── reader.go              git log/show parsing via os/exec
│   ├── tui/
│   │   └── tui.go                 Bubbletea TUI (heatmap + entry list)
│   └── mcpserver/
│       └── server.go              MCP server (5 tools)
├── go.mod
├── setup.sh
└── README.md

Data: ~/.local/share/diaryctl/diary.db (SQLite, WAL mode)
```

### Data flow

```
git repos → git reader → DayStats → entry builder → markdown template
                                                          ↓
                                               SQLite (entries table)
                                                          ↓
                                              TUI / CLI / MCP server
                                                          ↓
                                              Claude (via MCP) → filled narrative
```
