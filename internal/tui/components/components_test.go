package components

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/LeanMendez/pr-review/internal/tui/theme"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

func TestSizeBar(t *testing.T) {
	tests := []struct {
		lines int
		want  string
	}{
		{900, "████████│████»"},
		{393, "███████▉│     "},
		{252, "█████···│     "},
		{130, "██▋·····│     "},
		{43, "▉·······│     "},
		{400, "████████│     "},
		{450, "████████│█    "},
	}
	for _, tc := range tests {
		got := tuitest.Plain(SizeBar(tc.lines, 400))
		if got != tc.want {
			t.Errorf("SizeBar(%d) = %q, want %q", tc.lines, got, tc.want)
		}
		if w := ansi.StringWidth(got); w != 14 {
			t.Errorf("SizeBar(%d) width = %d, want 14", tc.lines, w)
		}
	}
}

func TestKeyHelp(t *testing.T) {
	got := tuitest.Plain(KeyHelp([]Key{{"↑↓", "moverse"}, {"q", "salir"}}))
	if want := "↑↓ moverse    q salir"; got != want {
		t.Errorf("KeyHelp = %q, want %q", got, want)
	}
}

func TestSizeBarGradient(t *testing.T) {
	t.Cleanup(func() {
		def, _ := theme.ByName(theme.DefaultName)
		theme.Use(def)
	})
	g, _ := theme.ByName("gemini")
	theme.Use(g)
	cells := tuitest.Cells(SizeBar(900, 400))
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		want := theme.GradientAt(g.Gradient, float64(i)/7, g.BG, 3)
		if c := cells[i]; c.Text != "█" || c.FG != want || c.BG != g.BG {
			t.Errorf("gemini cell %d = %+v, want █ fg %s bg %s", i, c, want, g.BG)
		}
		seen[cells[i].FG] = true
	}
	if len(seen) != 8 {
		t.Errorf("gemini bar has %d distinct colors, want 8", len(seen))
	}
	// a partial last cell takes its own position's color
	if c := tuitest.Cells(SizeBar(393, 400))[7]; c.Text != "▉" || c.FG != theme.GradientAt(g.Gradient, 1, g.BG, 3) {
		t.Errorf("gemini partial cell = %+v", c)
	}

	def, _ := theme.ByName(theme.DefaultName)
	theme.Use(def)
	for i, c := range tuitest.Cells(SizeBar(900, 400))[:8] {
		if c.FG != def.Dim || c.BG != def.BG {
			t.Errorf("pr-review cell %d = %+v, want fg %s bg %s", i, c, def.Dim, def.BG)
		}
	}
}
