package screens

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/tui/components"
	"github.com/LeanMendez/pr-review/internal/tui/listdata"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// ThemeChosenMsg asks the router to save and apply the theme Name.
type ThemeChosenMsg struct{ Name string }

const (
	// Lines of the picker without preview: header, blank, help.
	themesChrome = 3
	// The preview block is a blank line, its label and the list itself.
	previewExtra     = 2
	minPreviewHeight = 12 // below this the preview is replaced by a hint
	maxPreviewHeight = 16 // the list with 3 PRs needs no more
	previewHint      = "agrandá la terminal para ver la vista previa"
)

// ThemesModel is the theme picker: the 14 themes and a preview of the PR list
// painted with the highlighted one. It never changes the active theme; the
// router does when it gets ThemeChosenMsg.
type ThemesModel struct {
	themes        []theme.Theme
	cursor        int
	width, height int
}

// NewThemes builds the picker with current preselected, or the default theme
// when current is unknown.
func NewThemes(current string) ThemesModel {
	m := ThemesModel{themes: theme.All()}
	for i, t := range m.themes {
		if t.Name == current {
			m.cursor = i
		}
	}
	return m
}

// Init does nothing: the picker has nothing to load.
func (m ThemesModel) Init() tea.Cmd { return nil }

// Update handles window sizes and key presses.
func (m ThemesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, emit(QuitMsg{})
		case "up":
			m.cursor = max(0, m.cursor-1)
		case "down":
			m.cursor = min(len(m.themes)-1, m.cursor+1)
		case "enter":
			return m, emit(ThemeChosenMsg{Name: m.themes[m.cursor].Name})
		case "esc":
			return m, emit(BackMsg{})
		}
	}
	return m, nil
}

// View renders the screen at the last known size.
func (m ThemesModel) View() tea.View { return tea.NewView(m.render(m.width, m.height)) }

func (m ThemesModel) render(width, height int) string {
	n := len(m.themes)
	rows, previewH := n, 0
	if height > 0 {
		// Rows left for the list once the header, blank and help are placed.
		free := height - themesChrome
		if ph := min(maxPreviewHeight, free-n-previewExtra); ph >= minPreviewHeight {
			previewH = ph
		} else {
			free-- // the hint takes a line
			rows = max(1, min(n, free))
		}
	}
	top := max(0, min(m.cursor-rows/2, n-rows))

	lines := []string{" " + theme.Brand("pr-review") + "   " + theme.Dim.Render("configuración, tema"), ""}
	for i := top; i < top+rows; i++ {
		lines = append(lines, m.themeLine(i))
	}
	switch {
	case previewH > 0:
		lines = append(lines, "", theme.Dim.Render(" Vista previa:"))
		lines = append(lines, strings.Split(m.preview(width, previewH), "\n")...)
	case height > 0:
		lines = append(lines, theme.Dim.Render(" "+previewHint))
	}
	return finishScreen(lines, width, height, []components.Key{
		{Keys: "↑↓", Desc: "probar tema"}, {Keys: "enter", Desc: "usar este"}, {Keys: "esc", Desc: "volver sin cambiar"},
	})
}

func (m ThemesModel) themeLine(i int) string {
	t := m.themes[i]
	prefix, name := "   ", theme.Text
	if i == m.cursor {
		prefix, name = " "+theme.Marker()+" ", theme.Strong
	}
	line := prefix + name.Render(pad(t.Name, 16)) + theme.Dim.Render(clean(t.Desc))
	if t.Name == theme.Current().Name {
		line += theme.Dim.Render("  (actual)")
	}
	return line
}

// preview renders the real PR list with the sample rows in the highlighted
// theme; the active theme is back as soon as it returns.
func (m ThemesModel) preview(width, height int) string {
	return theme.With(m.themes[m.cursor], func() string {
		now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
		list := NewList("equipo/api-pagos", nil)
		list.now = func() time.Time { return now }
		loaded, _ := list.Update(RowsLoadedMsg{Ready: sampleRows(now)})
		return theme.Fill(loaded.(ListModel).render(width, height), width, height)
	})
}

// sampleRows are the three PRs of the mockups, ready for review.
func sampleRows(now time.Time) []listdata.Row {
	mk := func(id int, title, author string, lines int, age time.Duration) listdata.Row {
		return listdata.Row{
			Ctx: prb.PRContext{
				PullRequest: prb.PR{ID: id, Title: title, Author: author, UpdatedOn: now.Add(-age),
					Review: prb.Review{Approvals: 1, Reviewers: []prb.Reviewer{{Name: "a"}, {Name: "b"}}}},
				Checks: &prb.Checks{Overall: "pass", Passed: 3},
				Tasks:  &prb.Tasks{},
				Files:  &prb.Files{Count: 23, LinesAdded: lines * 3 / 4, LinesRemoved: lines / 4},
			},
			Lines:     lines,
			Suggested: pipeline.ModeExhaustivo,
		}
	}
	return []listdata.Row{
		mk(477, "Migrar facturas a nueva tabla", "Lucía G.", 900, 24*time.Hour),
		mk(88, "Refresh token con rotación", "Tomás V.", 393, 3*time.Hour),
		mk(1290, "Filtros de fecha en reportes", "Joaquín P.", 130, 5*time.Hour),
	}
}
