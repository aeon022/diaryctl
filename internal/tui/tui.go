package tui

import (
	"image/color"
	"os"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/diaryctl/internal/ai"
	"github.com/aeon022/diaryctl/internal/models"
	"github.com/aeon022/diaryctl/internal/store"
)

// ── Views ─────────────────────────────────────────────────────────────────────

type viewType int

const (
	listView   viewType = iota
	detailView viewType = iota
	editorView viewType = iota
	repoView   viewType = iota
	helpView   viewType = iota
)

// ── Messages ──────────────────────────────────────────────────────────────────

type (
	entriesLoadedMsg struct{ entries []models.Entry }
	reposLoadedMsg   struct{ repos []models.Repo }
	entryGenMsg      struct {
		entry *models.Entry
		err   error
	}
	autoSaveTickMsg struct{}
	errMsg          struct{ err error }

	aiChunkMsg struct{ chunk string }
	aiDoneMsg  struct{ full string }
	aiErrMsg   struct{ err error }

	todaySummaryMsg struct {
		commits  int
		tasks    int
		events   int
		duration time.Duration
	}
	animTickMsg struct{}
)

// adaptive resolves a light/dark color pair once, at startup — v2 dropped
// AdaptiveColor, and these package-level styles are built once, not per render.
var adaptive = func() func(light, dark string) color.Color {
	pick := lipgloss.LightDark(lipgloss.HasDarkBackground(os.Stdin, os.Stdout))
	return func(light, dark string) color.Color { return pick(lipgloss.Color(light), lipgloss.Color(dark)) }
}()

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	store   *store.Store
	view    viewType
	width   int
	height  int
	streak  int
	err     error
	loading bool
	sp      spinner.Model

	// flash message
	message string
	msgAt   time.Time

	// list
	entries      []models.Entry
	lastLoad     time.Time // last entries (re)load; throttles the window-focus reload
	cursor       int
	hoverRow     int // visibleEntries() index under the mouse cursor, -1 when none
	lastClickRow int // visibleEntries() index of the previous left-click, -1 when none — double-click opens the entry detail, same window/pattern taskctl uses
	lastClickAt  time.Time

	// search
	searching   bool
	searchQuery string
	searchRes   []models.Entry

	// ":" command palette
	inPalette     bool
	paletteQuery  string
	paletteCursor int

	// day-end activity prompt (ask mode): offered once per session
	actAsk      bool
	actAskShown bool
	actAskCount int

	// delete confirm
	confirmDelete bool
	deleteDate    time.Time
	deleteTarget  *models.Entry // full entry captured at "d" time, for undo

	// undo: "u" within undoWindow of a delete restores the deleted entry —
	// same pattern and window taskctl uses for its own delete-undo.
	lastDeleted *models.Entry

	// detail
	detail   *models.Entry
	detailVP viewport.Model

	// editor
	editorEntry     *models.Entry
	ta              textarea.Model
	editorDirty     bool
	lastSaved       time.Time
	savedFlash      bool
	centeredMode    bool
	wordGoal        int
	vimNormal       bool
	aiBeforeContent string

	// AI streaming
	aiGenerating bool
	aiTokens     int
	aiChan       chan ai.StreamResult

	// repos
	repos             []models.Repo
	repoCursor        int
	confirmDeleteRepo bool         // "d" was pressed once, waiting on y/Y to confirm — this view had no confirm step before, unlike every other destructive action in the app
	lastDeletedRepo   *models.Repo // undo: "u" within undoWindow restores it, same pattern as entry delete-undo

	// today summary (loaded async after repos)
	todayCommits  int
	todayTasks    int
	todayEvents   int
	todayDuration time.Duration
	todayLoaded   bool

	// animation tick counter
	tickCount int

	// "?" transient help popup
	helpVP   viewport.Model
	helpPopW int
	helpPopH int
}

func newTextarea() textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.Placeholder = ""
	st := ta.Styles()
	st.Focused.Base = lipgloss.NewStyle()
	st.Blurred.Base = lipgloss.NewStyle()
	st.Focused.CursorLine = lipgloss.NewStyle()
	st.Focused.Prompt = lipgloss.NewStyle()
	st.Blurred.Prompt = lipgloss.NewStyle()
	ta.SetStyles(st)
	return ta
}
