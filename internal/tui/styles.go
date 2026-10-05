package tui

import (
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/diaryctl/internal/store"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/theme"
)

// ── Design system ─────────────────────────────────────────────────────────────

var (
	// Shared across the suite via missionctl-core/theme.
	colorGreen = theme.GreenV2
	colorAmber = theme.AmberV2
	colorMuted = theme.MutedV2
	colorRed   = theme.RedV2
	selectedBg = theme.SelectedBgV2
	selectedFg = theme.SelectedFgV2
	colorBlue  = theme.BlueV2
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).Foreground(colorBlue).Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Background(selectedBg).Foreground(selectedFg).Padding(0, 1)
	// hoverStyle matches selectedStyle's Padding(0, 1) so a hovered row
	// renders at the same total width as a selected one (theme.Hover
	// itself carries no padding, since that's context-specific).
	hoverStyle = theme.HoverV2.Padding(0, 1)

	normalStyle = lipgloss.NewStyle().Padding(0, 1)
	mutedStyle  = lipgloss.NewStyle().Foreground(colorMuted)
	amberStyle  = lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	greenStyle  = lipgloss.NewStyle().Foreground(colorGreen)
	redStyle    = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	helpStyle   = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	statusStyle = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)
	// categoryStyle marks a recognized life-category tag (FH, Studium,
	// Projekt, Day-Job, ... — see diary.IsKnownCategory) so it reads as
	// "which part of life" at a glance, distinct from a plain project tag.
	categoryStyle = lipgloss.NewStyle().Foreground(colorBlue)
	// titleRowStyle marks an entry-list row whose preview is a real
	// parsed title (diary.ParseTitleTags), not just a muted body snippet.
	titleRowStyle = lipgloss.NewStyle().Bold(true)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).Padding(0, 1)

	editorBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorGreen).Padding(0, 1)
)

// ── command palette (":") ────────────────────────────────────────────────────
//
// Types out full words instead of memorizing single-key shortcuts. Reuses
// the exact same key handling every shortcut already goes through
// (handleList) by replaying the mapped keypress, so behavior is guaranteed
// identical to typing the key directly. Matching logic lives in
// missionctl-core/palette (shared across the suite); this list is
// diaryctl-specific.
var paletteCommands = []palette.Command{
	{Name: "new", Desc: "Generate today's entry", Key: "n"},
	{Name: "edit", Desc: "Edit entry", Key: "e"},
	{Name: "delete", Desc: "Delete entry (asks to confirm)", Key: "d"},
	{Name: "open", Desc: "Open entry", Key: "enter"},
	{Name: "copy", Desc: "Copy title to clipboard", Key: "y"},
	{Name: "undo", Desc: "Undo last delete", Key: "u"},
	{Name: "goto", Desc: "Open corresponding note in notectl", Key: "g"},
	{Name: "repos", Desc: "Browse tracked git repos", Key: "r"},
	{Name: "search", Desc: "Search entries", Key: "/"},
	{Name: "help", Desc: "Show help", Key: "?"},
	{Name: "quit", Desc: "Quit diaryctl", Key: "q"},
}

func New(s *store.Store) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = mutedStyle
	return &Model{
		store:        s,
		ta:           newTextarea(),
		wordGoal:     250,
		hoverRow:     -1,
		lastClickRow: -1,
		loading:      true,
		sp:           sp,
	}
}
