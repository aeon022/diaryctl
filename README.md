# diaryctl

Developer diary powered by git history and AI. Part of the [missionctl](https://missionctl.sh) suite.

Reads your commits, completed tasks, calendar events, and time logs. Generates a structured diary template. Press `ctrl+g` in the editor and Claude writes the narrative — or let the daemon handle it automatically at end of day.

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

### 1 — In-TUI (press `ctrl+g`)
Open any entry in the editor and press `ctrl+g`. Claude streams the narrative live into the `<!-- AI: -->` sections. Needs `ANTHROPIC_API_KEY` in your environment.

```
export ANTHROPIC_API_KEY=sk-ant-...
diaryctl          # open TUI → select entry → e → ctrl+g
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
- **habctl** — today's habit check-ins and streaks

All three appear as sections in the diary template. diaryctl reads each sister app's
SQLite database directly and read-only (`internal/suite`) — it never shells out to the
other CLIs. It finds each database the way that tool does: `<TOOL>_DATA_DIR` from your
environment first, then `data_dir` in taskctl's / calctl's config file, else the default
location. If a sister database doesn't exist yet (app not installed, or `sync` never
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
ctrl+g         ask Claude to write the narrative (streams live)
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
~/.local/share/diaryctl/diary.db     SQLite (WAL mode by default — see below for syncing)
~/.local/share/diaryctl/logs/        Daemon logs
```

### Syncing across devices

To share your diary across devices, set `DIARYCTL_DATA_DIR` to a folder you already sync yourself — iCloud Drive, Dropbox, Syncthing, etc. (diaryctl has no config-file setting for this, only the env var):

```bash
export DIARYCTL_DATA_DIR="$HOME/Library/Mobile Documents/com~apple~CloudDocs/diaryctl"
```

Once set, diaryctl automatically switches its SQLite journal mode from WAL to rollback-journal — WAL splits the database across multiple files that a folder-sync client can't update atomically together, so this switch keeps the directory down to a single consistent file whenever diaryctl isn't actively writing. A same-machine lock also prevents two diaryctl processes from opening the database at once (run `diaryctl doctor` to see the current mode and path). This only protects against the same-machine and stale-snapshot failure modes, not two machines editing at the exact same instant; an undownloaded iCloud file is reported explicitly rather than as a bare error.

## Activity in your diary

Every tool in the suite appends what you *do* (task completed, habit checked, note
written, timer stopped, …) to a shared activity log
(`~/.local/share/missionctl/activity.jsonl`, titles only — no note bodies, mail text or
amounts; `MISSIONCTL_DATA_DIR` moves it, `MISSIONCTL_ACTIVITY=off` or `enabled: false` in
`~/.config/missionctl/activity.yaml` turns logging off). diaryctl can put the day's
activity into that day's entry as a block like this:

```markdown
<!-- activity:start -->
## Activity

- 14:05 taskctl · completed — Steuer abgeben
- 18:30 habctl · checked — Sport
<!-- activity:end -->
```

Only the text between the two markers is ever replaced; the rest of your entry is never
touched, and running it again refreshes the block instead of adding another.

How it happens is a setting (`diary:` in `activity.yaml`, or `diaryctl activity --mode`):

| Mode | Behavior |
|------|----------|
| `ask` (default) | In the TUI, after 18:00, when today's entry exists without a block and there is activity, a popup asks once per session: `y` add now · `n` / `esc` not now (asked again next session) · `a` always (switches to `auto` and adds now) · `x` never (switches to `off`). Not shown while editing, searching, in the palette or in a delete confirmation. |
| `auto` | The daily daemon run (`diaryctl daemon generate`, default 17:00 — see `diaryctl daemon start --hour`) adds the block to a new entry, and refreshes only the block of an existing one. |
| `off` | Nothing is added automatically. |

```sh
diaryctl activity                  # add / refresh today's block now (any mode)
diaryctl activity --date 2026-10-05
diaryctl activity --mode auto      # ask | auto | off
```

diaryctl itself logs `wrote` once per entry date and session when you save an entry
(editor, `diaryctl today` editing, MCP `write_diary_entry`) — not for generated templates.

## Recent changes (October 2026)

- **Window focus.** When the terminal window regains focus, the list reloads from the local database — at most every 5 seconds, and only while you are just browsing (never while a form, editor, search, palette or confirmation is open, so nothing you are typing is lost). Terminals that don't report focus events simply never trigger it. The editor is never reloaded.

- **Clipboard.** `y` copies the selected entry's date — now through OSC 52 as well as `pbcopy`, so it also works over SSH and inside tmux (your terminal must allow OSC 52; locally `pbcopy` still does the job).

- **Footer and empty states.** The key-hint footer is the suite-wide one: it never wraps and drops the least important hints first on narrow terminals. Empty lists and loading screens show a short message with a hint what to press.

- **AI key is now `ctrl+g`.** In the editor (insert and vim-normal mode) `ctrl+g` asks Claude to continue the entry. The plain letter `a` used to start the AI, so every `a` you typed in the text triggered it.

- **Typing.** The search box and command palette now accept spaces and umlauts, and backspace removes a whole character.

- **Other tools' data.** The suite sections (tasks, events, time, habits) are read from each tool's real database: `<TOOL>_DATA_DIR` first (e.g. `TASKCTL_DATA_DIR`), then `data_dir` in taskctl's and calctl's config file, else the tool's default location — so data kept in Dropbox/iCloud is found. "Today" is your local day, not UTC.

- **Streaks.** The diary streak no longer bridges gaps and no longer reads 0 shortly after local midnight; archived habits are not listed; a `|` in a commit subject no longer corrupts the git summary.

- The TUI now runs on Bubble Tea v2; key bindings are unchanged.

---

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
