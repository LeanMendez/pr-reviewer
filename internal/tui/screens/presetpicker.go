package screens

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/tui/components"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// PresetChosenMsg reports a ready-made preset picked in the preset picker.
type PresetChosenMsg struct{ Preset config.Preset }

// CustomChosenMsg reports that the user wants to pick models role by role.
type CustomChosenMsg struct{}

type presetOption struct {
	preset      config.Preset
	name, desc  string
	recommended bool
}

var presetOptions = []presetOption{
	{config.PresetBalanceado, "Balanceado", "opus para pensar y buscar bugs, sonnet para reglas y tests", true},
	{config.PresetEconomico, "Económico", "sonnet casi en todo: gasta menos de tu límite, encuentra menos", false},
	{config.PresetPotente, "Potente", "opus en todo y con más esfuerzo: gasta bastante más", false},
	{config.PresetPersonalizado, "Personalizado", "elegís modelo y esfuerzo para cada agente", false},
}

// PresetPickerModel lets the user pick a preset and previews its assignments.
type PresetPickerModel struct {
	cfg    config.Config
	cursor int

	width, height int
}

// NewPresetPicker builds the picker with cfg's current preset highlighted
// (Balanceado when it has none).
func NewPresetPicker(cfg config.Config) PresetPickerModel {
	m := PresetPickerModel{cfg: cfg}
	for i, o := range presetOptions {
		if o.preset == cfg.Preset {
			m.cursor = i
		}
	}
	return m
}

// Init does nothing: the picker has nothing to load.
func (m PresetPickerModel) Init() tea.Cmd { return nil }

// Update handles window sizes and key presses.
func (m PresetPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			m.cursor = min(len(presetOptions)-1, m.cursor+1)
		case "enter":
			if p := presetOptions[m.cursor].preset; p != config.PresetPersonalizado {
				return m, emit(PresetChosenMsg{Preset: p})
			}
			return m, emit(CustomChosenMsg{})
		case "esc":
			return m, emit(BackMsg{})
		}
	}
	return m, nil
}

// View renders the screen at the last known size.
func (m PresetPickerModel) View() tea.View { return tea.NewView(m.render(m.width, m.height)) }

func (m PresetPickerModel) render(width, height int) string {
	cur := presetOptions[m.cursor]
	lines := []string{
		" " + theme.Brand("pr-review") + theme.Dim.Render("   configuración inicial, paso 3 de 3"),
		"",
		theme.Strong.Render("  Elegí qué modelos usan los agentes."),
		theme.Dim.Render("  Después lo podés cambiar desde la lista con la tecla m."),
		"",
	}
	for i, o := range presetOptions {
		mark, dot, name := "  ", theme.Dim.Render("○ "), theme.Text
		if i == m.cursor {
			mark, dot, name = " "+theme.Marker(), theme.Accent.Render("● "), theme.Strong
		}
		rec := strings.Repeat(" ", len("recomendado")+2)
		if o.recommended {
			rec = theme.OK.Render("recomendado") + "  "
		}
		desc := theme.Dim.Render(o.desc)
		if i == m.cursor {
			desc = theme.Text.Render(o.desc)
		}
		lines = append(lines, mark+dot+name.Render(fmt.Sprintf("%-14s", o.name))+rec+desc)
	}
	title := "Así queda " + cur.name + ":"
	assign := config.PresetAssignments(cur.preset)
	if cur.preset == config.PresetPersonalizado {
		title = "Así está ahora tu configuración:"
		assign = map[config.Role]config.Assignment{}
		for _, r := range config.AllRoles() {
			assign[r] = m.cfg.AssignmentFor(r)
		}
	}
	lines = append(lines, "", theme.Dim.Render("  "+title))
	roles := config.AllRoles()
	half := (len(roles) + 1) / 2
	for i := 0; i < half; i++ {
		l := "    " + previewCell(roles[i], assign)
		if i+half < len(roles) {
			l += previewCell(roles[i+half], assign)
		}
		lines = append(lines, l)
	}
	lines = append(lines, "",
		theme.Dim.Render("  Uso por revisión: ligero 4 agentes, exhaustivo 9, con judgment 11."),
		"",
		"  "+components.KeyHelp([]components.Key{
			{Keys: "↑↓", Desc: "elegir"}, {Keys: "enter", Desc: "confirmar"}, {Keys: "esc", Desc: "volver al paso anterior"},
		}))
	return renderLines(lines, width, height)
}

func previewCell(r config.Role, assign map[config.Role]config.Assignment) string {
	name, _ := roleLabel(r)
	a := assign[r]
	return theme.Text.Render(fmt.Sprintf("%-21s", name)) +
		theme.Ref.Render(fmt.Sprintf("%-8s", a.Model)) +
		theme.Dim.Render(fmt.Sprintf("%-9s", effortLabel(a.Effort)))
}

// renderLines pads or crops lines to height (0 means as is) and fits each to
// width.
func renderLines(lines []string, width, height int) string {
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	for i, l := range lines {
		lines[i] = fit(l, width)
	}
	return strings.Join(lines, "\n")
}

// effortLabel is the Spanish name of an effort level.
func effortLabel(e config.Effort) string {
	switch e {
	case config.EffortLow:
		return "bajo"
	case config.EffortMedium:
		return "medio"
	case config.EffortHigh:
		return "alto"
	case config.EffortXHigh:
		return "muy alto"
	case config.EffortMax:
		return "máximo"
	}
	return "—"
}
