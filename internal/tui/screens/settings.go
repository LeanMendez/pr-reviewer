package screens

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/tui/components"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// OpenModelsMsg asks the router to open the models screen.
type OpenModelsMsg struct{}

// OpenThemesMsg asks the router to open the theme picker.
type OpenThemesMsg struct{}

var settingsOptions = []struct{ name, desc string }{
	{"Modelos", "qué modelo usa cada agente"},
	{"Tema", "los colores de la interfaz"},
}

// SettingsModel is the settings menu: Modelos and Tema.
type SettingsModel struct {
	cursor        int
	width, height int
}

// NewSettings builds the menu with Modelos selected.
func NewSettings() SettingsModel { return SettingsModel{} }

// Init does nothing: the menu has nothing to load.
func (m SettingsModel) Init() tea.Cmd { return nil }

// Update handles window sizes and key presses.
func (m SettingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			m.cursor = min(len(settingsOptions)-1, m.cursor+1)
		case "enter":
			if m.cursor == 0 {
				return m, emit(OpenModelsMsg{})
			}
			return m, emit(OpenThemesMsg{})
		case "esc":
			return m, emit(BackMsg{})
		}
	}
	return m, nil
}

// View renders the screen at the last known size.
func (m SettingsModel) View() tea.View { return tea.NewView(m.render(m.width, m.height)) }

func (m SettingsModel) render(width, height int) string {
	lines := []string{" " + theme.Brand("pr-review") + "   " + theme.Dim.Render("configuración"), ""}
	for i, o := range settingsOptions {
		prefix, name := "   ", theme.Text
		if i == m.cursor {
			prefix, name = " "+theme.Marker()+" ", theme.Strong
		}
		lines = append(lines, prefix+name.Render(pad(o.name, 10))+theme.Dim.Render(o.desc))
	}
	return finishScreen(lines, width, height, []components.Key{
		{Keys: "↑↓", Desc: "moverse"}, {Keys: "enter", Desc: "elegir"}, {Keys: "esc", Desc: "volver"},
	})
}

// finishScreen pads lines down to the key help on the last row of height
// (when known), keeps the bottom height lines and fits each to width.
func finishScreen(lines []string, width, height int, keys []components.Key) string {
	if height > 0 {
		for len(lines) < height-1 {
			lines = append(lines, "")
		}
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, " "+components.KeyHelp(keys))
	if height > 0 && len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	for i, l := range lines {
		lines[i] = fit(l, width)
	}
	return strings.Join(lines, "\n")
}
