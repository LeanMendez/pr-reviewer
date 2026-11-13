package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/tui/theme"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

func restoreTheme(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		def, _ := theme.ByName(theme.DefaultName)
		theme.Use(def)
	})
}

func pressThemes(m ThemesModel, key string) (ThemesModel, tea.Msg) {
	n, cmd := m.Update(tuitest.Key(key))
	var msg tea.Msg
	if cmd != nil {
		msg = cmd()
	}
	return n.(ThemesModel), msg
}

func TestThemesListAndPreview(t *testing.T) {
	restoreTheme(t)
	m := NewThemes("pr-review")
	out := m.render(100, 40)
	plain := tuitest.Plain(out)
	for _, th := range theme.All() {
		if !strings.Contains(plain, th.Name) || !strings.Contains(plain, th.Desc) {
			t.Errorf("list lacks theme %q or its description", th.Name)
		}
	}
	for _, want := range []string{"477", "Migrar facturas"} {
		if !strings.Contains(plain, want) {
			t.Errorf("preview lacks %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "agrandá la terminal") {
		t.Error("preview should fit at 100x40")
	}

	for range 4 {
		m, _ = pressThemes(m, "down")
	}
	lines := strings.Split(m.render(100, 40), "\n")
	var row string
	for _, l := range lines {
		if strings.Contains(tuitest.Plain(l), "Migrar facturas") {
			row = l
		}
	}
	if row == "" {
		t.Fatal("no preview row")
	}
	for _, c := range tuitest.Cells(row) {
		if c.BG != "#1E1E2E" {
			t.Fatalf("preview cell %q has bg %q, want gemini #1E1E2E", c.Text, c.BG)
		}
	}
	if got := theme.Current().Name; got != theme.DefaultName {
		t.Errorf("active theme = %q, want it untouched", got)
	}
}

func TestThemesEnterEsc(t *testing.T) {
	restoreTheme(t)
	m := NewThemes("pr-review")
	for range 4 {
		m, _ = pressThemes(m, "down")
	}
	_, msg := pressThemes(m, "enter")
	if msg != (ThemeChosenMsg{Name: "gemini"}) {
		t.Errorf("enter = %v, want ThemeChosenMsg{gemini}", msg)
	}
	if _, msg = pressThemes(m, "esc"); msg != (BackMsg{}) {
		t.Errorf("esc = %v, want BackMsg", msg)
	}
	if got := theme.Current().Name; got != theme.DefaultName {
		t.Errorf("active theme = %q, want it untouched", got)
	}
}

func TestThemesUnknownPreselectsDefault(t *testing.T) {
	restoreTheme(t)
	_, msg := pressThemes(NewThemes("vaporwave"), "enter")
	if msg != (ThemeChosenMsg{Name: theme.DefaultName}) {
		t.Errorf("enter = %v, want the default theme", msg)
	}
}

func TestThemesShortTerminal(t *testing.T) {
	restoreTheme(t)
	m := NewThemes("pr-review")
	for range 12 {
		m, _ = pressThemes(m, "down")
	}
	out := m.render(80, 12)
	lines := strings.Split(out, "\n")
	if len(lines) > 12 {
		t.Errorf("%d lines, want at most 12", len(lines))
	}
	for _, l := range lines {
		if w := len([]rune(tuitest.Plain(l))); w > 80 {
			t.Errorf("line wider than 80: %q", tuitest.Plain(l))
		}
	}
	plain := tuitest.Plain(out)
	if !strings.Contains(plain, theme.All()[12].Name) {
		t.Errorf("selected theme not visible:\n%s", plain)
	}
	if !strings.Contains(plain, "agrandá la terminal para ver la vista previa") {
		t.Errorf("missing small-terminal hint:\n%s", plain)
	}
}
