package screens

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/tui/components"
	"github.com/LeanMendez/pr-review/internal/tui/listdata"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// StartReviewMsg asks the router to start reviewing Row in Mode.
type StartReviewMsg struct {
	Row  listdata.Row
	Mode pipeline.Mode
}

// BackMsg asks the router to go back to the previous screen. Every screen
// that can go back emits it.
type BackMsg struct{}

var modeOrder = []pipeline.Mode{pipeline.ModeLigero, pipeline.ModeExhaustivo, pipeline.ModeJudgment}

// ModeModel is the mode picker shown after choosing a PR.
type ModeModel struct {
	row    listdata.Row
	cfg    config.Config
	cursor int

	width, height int
}

// NewMode builds the picker for row with the suggested mode preselected; cfg
// decides which models the summary shows.
func NewMode(row listdata.Row, cfg config.Config) ModeModel {
	m := ModeModel{row: row, cfg: cfg}
	for i, md := range modeOrder {
		if md == row.Suggested {
			m.cursor = i
		}
	}
	return m
}

// Init does nothing: the picker has nothing to load.
func (m ModeModel) Init() tea.Cmd { return nil }

// Update handles window sizes and key presses.
func (m ModeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			m.cursor = min(len(modeOrder)-1, m.cursor+1)
		case "enter":
			return m, emit(StartReviewMsg{Row: m.row, Mode: modeOrder[m.cursor]})
		case "esc":
			return m, emit(BackMsg{})
		}
	}
	return m, nil
}

// View renders the screen at the last known size.
func (m ModeModel) View() tea.View { return tea.NewView(m.render(m.width, m.height)) }

func (m ModeModel) render(width, height int) string {
	pr := m.row.Ctx.PullRequest
	lines := []string{
		" " + theme.Brand("pr-review") + "   " + theme.Dim.Render(fmt.Sprintf("#%d  %s", pr.ID, clean(pr.Title))),
		"",
		theme.Text.Render(" Elegí cómo revisarlo"),
		"",
	}
	for i, md := range modeOrder {
		lines = append(lines, m.modeLines(md, i == m.cursor)...)
		lines = append(lines, "")
	}
	lines = append(lines, theme.Dim.Render(" Los agentes son un piso: en PRs grandes pueden sumarse más."))
	if height > 0 {
		for len(lines) < height-1 {
			lines = append(lines, "")
		}
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, " "+components.KeyHelp([]components.Key{
		{Keys: "↑↓", Desc: "moverse"}, {Keys: "enter", Desc: "elegir y empezar"}, {Keys: "esc", Desc: "volver"},
	}))
	if height > 0 && len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	for i, l := range lines {
		lines[i] = fit(l, width)
	}
	return strings.Join(lines, "\n")
}

func (m ModeModel) modeLines(md pipeline.Mode, selected bool) []string {
	prefix, name := "   ", theme.Text
	if selected {
		prefix, name = " "+theme.Marker()+" ", theme.Strong
	}
	title := prefix + name.Render(string(md))
	if md == m.row.Suggested {
		title += theme.OK.Render("  sugerido")
	}
	var stages []string
	for _, s := range pipeline.Stages(md) {
		stages = append(stages, s.Name())
	}
	const indent = "     "
	return []string{
		title,
		theme.Dim.Render(indent + strings.Join(stages, " → ")),
		theme.Dim.Render(fmt.Sprintf("%sunos %d agentes · %s", indent, pipeline.EstimateAgents(md), m.models(md))),
	}
}

// models summarizes the models of the main roles of md, from cfg.
func (m ModeModel) models(md pipeline.Mode) string {
	groups := []struct {
		label string
		roles []config.Role
	}{
		{"especialistas", []config.Role{config.RoleBugs, config.RoleReglas}},
		{"superior", []config.Role{config.RoleSuperior}},
	}
	if md == pipeline.ModeExhaustivo || md == pipeline.ModeJudgment {
		groups[0].roles = append(groups[0].roles, config.RoleSeguridad, config.RoleTests)
	}
	if md == pipeline.ModeJudgment {
		groups = append(groups, struct {
			label string
			roles []config.Role
		}{"jueces", []config.Role{config.RoleJuezA, config.RoleJuezB}})
	}
	var parts []string
	for _, g := range groups {
		var names []string
		seen := map[config.Model]bool{}
		for _, r := range g.roles {
			if mod := m.cfg.AssignmentFor(r).Model; !seen[mod] {
				seen[mod] = true
				names = append(names, string(mod))
			}
		}
		parts = append(parts, g.label+": "+joinY(names))
	}
	return strings.Join(parts, " · ")
}

// joinY joins names as "a", "a y b" or "a, b y c".
func joinY(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " y " + names[len(names)-1]
}
