package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

func TestSettingsMenu(t *testing.T) {
	m := NewSettings()
	n, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = n.(SettingsModel)
	out := tuitest.Plain(m.render(80, 20))
	for _, want := range []string{"Modelos", "Tema", "esc volver"} {
		if !strings.Contains(out, want) {
			t.Errorf("settings menu lacks %q:\n%s", want, out)
		}
	}
	press := func(m SettingsModel, key string) (SettingsModel, tea.Msg) {
		n, cmd := m.Update(tuitest.Key(key))
		var msg tea.Msg
		if cmd != nil {
			msg = cmd()
		}
		return n.(SettingsModel), msg
	}
	m, msg := press(m, "enter")
	if _, ok := msg.(OpenModelsMsg); !ok {
		t.Errorf("enter on Modelos = %T, want OpenModelsMsg", msg)
	}
	m, _ = press(m, "down")
	m, msg = press(m, "enter")
	if _, ok := msg.(OpenThemesMsg); !ok {
		t.Errorf("enter on Tema = %T, want OpenThemesMsg", msg)
	}
	if _, msg = press(m, "esc"); msg != (BackMsg{}) {
		t.Errorf("esc = %v, want BackMsg", msg)
	}
}
