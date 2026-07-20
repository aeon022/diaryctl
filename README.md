# diaryctl

Developer diary powered by git history and AI. Part of the [missionctl](https://missionctl.sh) suite.

Reads your commits, completed tasks, calendar events, and time logs. Generates a structured diary template. Press `a` in the editor and Claude writes the narrative — or let the daemon handle it automatically at end of day.

---

## Quick Start

```bash
# Install
bash setup.sh

# Register a git repo
diaryctl init ~/code/myproject --name "My Project"

# Generate today's entry
diaryctl today

# Open TUI (heatmap + editor)
diaryctl
```

### MCP (Claude Desktop)

```json
{
  "mcpServers": {
    "diaryctl": { "command": "diaryctl", "args": ["mcp"] }
  }
}
```

---

## AI Integration

Three ways to let Claude write the narrative:

### 1 — In-TUI (press `a`)
Open any entry in the editor and press `a`. Claude streams the narrative live into the `<!-- AI: -->` sections. Needs `ANTHROPIC_API_KEY` in your environment.

```
export ANTHROPIC_API_KEY=sk-ant-...
diaryctl          # open TUI → select entry → e → a
```

### 2 — Auto-daemon (fully hands-free)
Installs a launchd job that runs at 17:30 every day. If `ANTHROPIC_API_KEY` is set, Claude writes the full entry automatically and sends a macOS notification.

```bash
diaryctl daemon start              # daily at 17:30
diaryctl daemon start --hour 18    # custom time
diaryctl daemon stop
diaryctl daemon status
```

**With API key:** generates template → Claude writes → saves → notification "Entry is written"  
**Without API key:** generates template → saves → notification "Open Claude Desktop to write"

### 3 — MCP / Claude Desktop
Say "write my diary for today" in Claude Desktop. Claude calls `get_today_stats`, writes the narrative, calls `write_diary_entry`. No API key needed in diaryctl — Claude Desktop handles it.

### Suite Integration
When other missionctl apps are installed, diaryctl automatically pulls in:
- **taskctl** — completed tasks for today
- **calctl** — calendar events for today
- **timectl** — time log entries for today

All three appear as sections in the diary template. diaryctl reads each sister app's
SQLite database directly and read-only (`internal/suite`) — it never shells out to the
other CLIs. If a sister database doesn't exist yet (app not installed, or `sync` never
run), that section is simply omitted: no error, no crash, nothing to configure. Once
the other app is installed and has synced data, its section appears automatically on
the next entry generation.

---

## CLI Reference

| Command | Description |
|---|---|
| `diaryctl` | Open TUI |
| `diaryctl init [PATH] [--name NAME]` | Register a git repo |
| `diaryctl repos` | List registered repos |
| `diaryctl today [--open] [--json]` | Generate/show today's entry |
| `diaryctl list [--limit 30]` | List past entries |
| `diaryctl show [DATE]` | Show entry (YYYY-MM-DD, default: today) |
| `diaryctl stats [--days 30]` | Productivity stats + bar chart |
| `diaryctl daemon start [--hour H] [--minute M]` | Install launchd daemon |
| `diaryctl daemon stop` | Remove daemon |
| `diaryctl daemon status` | Show daemon state |
| `diaryctl mcp` | Start MCP server (stdio) |

---

## TUI Reference

### List view

```
j / k          navigate entries
enter          open detail view
e              open in editor
n              generate today's entry
d              delete entry (y to confirm)
/              search entries
r              manage repos
q              quit
```

### Editor (press `e` from list or detail)

```
ctrl+s         save
esc            save and back to list
a              ask Claude to write the narrative (streams live)
tab            jump to next <!-- AI: --> block
[ / ]          jump to previous / next ## section
ctrl+f         toggle centered writing mode (72-char column)
```

Status bar shows: date · word count · current section · save indicator · AI spinner while generating.

### Detail view

```
j / k          scroll
e              open in editor
d              delete entry
esc            back to list
```

### Repos view (press `r`)

```
j / k          navigate
d              delete repo
esc            back
```

---

## MCP Tools

| Tool | Parameters | Description |
|---|---|---|
| `get_today_stats` | — | Git commits, suite data (tasks, events, time) for today |
| `get_diary_entry` | `date` (YYYY-MM-DD, optional) | Read diary entry for a date |
| `write_diary_entry` | `body`, `date` (optional) | Save / overwrite entry |
| `get_coding_stats` | `days` (default 7) | Streak, commits, repo breakdown for N days |
| `list_diary_entries` | `limit` (default 10) | List entries with preview |

---

## Data

```
~/.local/share/diaryctl/diary.db     SQLite (WAL mode)
~/.local/share/diaryctl/logs/        Daemon logs
```

## Architecture

```
diaryctl/
├── cmd/
│   ├── root.go        TUI by default
│   ├── today.go       Generate entry
│   ├── daemon.go      launchd integration + AI auto-fill
│   └── ...
├── internal/
│   ├── ai/
│   │   └── claude.go  Anthropic SDK — Fill() + Stream()
│   ├── diary/
│   │   └── builder.go Template builder (git + suite → markdown)
│   ├── suite/
│   │   └── reader.go  Reads taskctl / calctl / timectl DBs
│   ├── git/
│   │   └── reader.go  git log / shortstat parsing
│   ├── tui/
│   │   └── tui.go     Bubbletea — heatmap, editor, AI streaming
│   ├── mcpserver/
│   │   └── server.go  5 MCP tools
│   └── store/
│       └── sqlite.go  SQLite CRUD
└── main.go
```
