package screens

import (
	"fmt"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/tui/components"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// ModelsSavedMsg reports that the configuration was saved.
type ModelsSavedMsg struct{ Cfg config.Config }

var (
	modelOrder  = []config.Model{config.ModelFable, config.ModelOpus, config.ModelSonnet, config.ModelHaiku}
	presetCycle = []config.Preset{config.PresetBalanceado, config.PresetEconomico, config.PresetPotente}
)

// ModelsModel is the per-role model and effort table.
type ModelsModel struct {
	cfg     config.Config
	save    func(config.Config) error
	start   map[config.Role]config.Assignment
	current map[config.Role]config.Assignment
	roles   []config.Role
	cursor  int
	// presetAt is the position in presetCycle of the last preset applied
	// (or of the starting preset); -1 when none.
	presetAt int
	err      error

	width, height int
}

// NewModels builds the table from cfg; save persists the result on "g".
func NewModels(cfg config.Config, save func(config.Config) error) ModelsModel {
	m := ModelsModel{
		cfg: cfg, save: save, roles: config.AllRoles(),
		start:    map[config.Role]config.Assignment{},
		current:  map[config.Role]config.Assignment{},
		presetAt: slices.Index(presetCycle, cfg.Preset),
	}
	for _, r := range m.roles {
		a := cfg.AssignmentFor(r)
		m.start[r], m.current[r] = a, a
	}
	return m
}

// Init does nothing: the table has nothing to load.
func (m ModelsModel) Init() tea.Cmd { return nil }

// Update handles window sizes and key presses.
func (m ModelsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		m.err = nil
		role := m.roles[m.cursor]
		switch msg.String() {
		case "ctrl+c":
			return m, emit(QuitMsg{})
		case "esc":
			return m, emit(BackMsg{})
		case "up":
			m.cursor = max(0, m.cursor-1)
		case "down":
			m.cursor = min(len(m.roles)-1, m.cursor+1)
		case "left", "right":
			m.current = m.withAssignment(role, m.cycleModel(m.current[role], msg.String() == "right"))
		case "e":
			m.current = m.withAssignment(role, cycleEffort(m.current[role]))
		case "p":
			m.presetAt = (m.presetAt + 1) % len(presetCycle)
			m.current = config.PresetAssignments(presetCycle[m.presetAt])
		case "g":
			out := m.result()
			if err := m.save(out); err != nil {
				m.err = err
				return m, nil
			}
			return m, emit(ModelsSavedMsg{Cfg: out})
		}
	}
	return m, nil
}

func (m ModelsModel) withAssignment(r config.Role, a config.Assignment) map[config.Role]config.Assignment {
	out := make(map[config.Role]config.Assignment, len(m.current))
	for k, v := range m.current {
		out[k] = v
	}
	out[r] = a
	return out
}

// cycleModel moves a to the next or previous model, clearing the effort when
// the new model does not support it.
func (m ModelsModel) cycleModel(a config.Assignment, forward bool) config.Assignment {
	i := slices.Index(modelOrder, a.Model)
	if forward {
		i = min(len(modelOrder)-1, i+1)
	} else {
		i = max(0, i-1)
	}
	a.Model = modelOrder[i]
	if !slices.Contains(config.EffortsFor(a.Model), a.Effort) {
		a.Effort = config.EffortDefault
	}
	return a
}

// cycleEffort moves a to the next effort its model supports.
func cycleEffort(a config.Assignment) config.Assignment {
	efforts := config.EffortsFor(a.Model)
	if len(efforts) == 0 {
		return a
	}
	a.Effort = efforts[(slices.Index(efforts, a.Effort)+1)%len(efforts)]
	return a
}

func (m ModelsModel) changed(r config.Role) bool { return m.current[r] != m.start[r] }

func (m ModelsModel) changes() int {
	n := 0
	for _, r := range m.roles {
		if m.changed(r) {
			n++
		}
	}
	return n
}

// result is cfg with the edited assignments; the preset is the starting one
// when nothing changed and personalizado otherwise.
func (m ModelsModel) result() config.Config {
	out := m.cfg
	out.Models = make(map[config.Role]config.Assignment, len(m.current))
	for k, v := range m.current {
		out.Models[k] = v
	}
	if m.changes() > 0 {
		out.Preset = config.PresetPersonalizado
	}
	return out
}

// View renders the screen at the last known size.
func (m ModelsModel) View() tea.View { return tea.NewView(m.render(m.width, m.height)) }

func (m ModelsModel) render(width, height int) string {
	from := "tu configuración"
	for _, o := range presetOptions {
		if o.preset == m.cfg.Preset && o.preset != config.PresetPersonalizado {
			from = o.name
		}
	}
	n := m.changes()
	cambios := fmt.Sprintf("%d cambios", n)
	if n == 1 {
		cambios = "1 cambio"
	}
	lines := []string{
		theme.Strong.Render(" pr-review") + theme.Dim.Render("   configuración, modelos      ") +
			theme.Dim.Render(fmt.Sprintf("partiste de %s, con %s", from, cambios)),
		"",
		theme.Dim.Render(fmt.Sprintf("    %-21s%-38s%-12s%s", "agente", "qué hace", "modelo", "esfuerzo")),
	}
	for i, r := range m.roles {
		lines = append(lines, m.row(r, i == m.cursor))
	}
	path := "~/.config/pr-review/config.yaml"
	if p, err := config.Path(); err == nil {
		if home, err := os.UserHomeDir(); err == nil {
			p = strings.Replace(p, home, "~", 1)
		}
		path = p
	}
	lines = append(lines, "",
		theme.Dim.Render("  Modelos: fable, opus, sonnet, haiku. Esfuerzo: bajo, medio, alto, muy alto, máximo."),
		theme.Dim.Render("  Se guarda en "+path))
	if m.err != nil {
		lines = append(lines, theme.Accent.Render("  No se pudo guardar: "+clean(m.err.Error())))
	} else {
		lines = append(lines, "")
	}
	help := "  " + components.KeyHelp([]components.Key{
		{Keys: "↑↓", Desc: "agente"}, {Keys: "←→", Desc: "modelo"}, {Keys: "e", Desc: "esfuerzo"},
		{Keys: "p", Desc: "usar una combinación armada"}, {Keys: "g", Desc: "guardar"}, {Keys: "esc", Desc: "salir sin guardar"},
	})
	if height > 0 {
		for len(lines) < height-1 {
			lines = append(lines, "")
		}
	}
	return renderLines(append(lines, help), width, 0)
}

func (m ModelsModel) row(r config.Role, selected bool) string {
	name, desc := roleLabel(r)
	a := m.current[r]
	mark, nameStyle := "    ", theme.Text
	model := theme.Ref.Render(fmt.Sprintf("  %-10s", a.Model))
	if selected {
		mark, nameStyle = " "+theme.Accent.Render("▌")+"  ", theme.Strong
		model = theme.Accent.Render(fmt.Sprintf("‹ %-6s ›  ", a.Model))
	}
	effort := theme.Text.Render(fmt.Sprintf("%-9s", effortLabel(a.Effort)))
	if a.Effort == config.EffortDefault {
		effort = theme.Dim.Render(fmt.Sprintf("%-9s", effortLabel(a.Effort)))
	}
	line := mark + nameStyle.Render(fmt.Sprintf("%-21s", name)) + theme.Dim.Render(fmt.Sprintf("%-38s", desc)) + model + " " + effort
	if m.changed(r) {
		line += theme.Accent.Render("  cambiado")
	}
	return line
}
