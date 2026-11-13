package theme

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

// useTheme activates the named theme for the rest of the test and restores
// the default afterwards, so tests stay order-independent.
func useTheme(t *testing.T, name string) Theme {
	t.Helper()
	th, ok := ByName(name)
	if !ok {
		t.Fatalf("no theme %q", name)
	}
	t.Cleanup(useDefault)
	Use(th)
	return th
}

func useDefault() {
	def, _ := ByName(DefaultName)
	Use(def)
}

func TestUseSwapsStyles(t *testing.T) {
	th := useTheme(t, "sensei")
	if Current().Name != "sensei" {
		t.Errorf("Current() = %q, want sensei", Current().Name)
	}
	if c := tuitest.Cells(Text.Render("x")); len(c) != 1 || c[0].FG != "#2C2C2C" || c[0].BG != "#F5F2EB" {
		t.Errorf("Text.Render under sensei = %+v", c)
	}
	tokens := map[string]struct {
		style lipgloss.Style
		fg    string
	}{
		"Text": {Text, th.Text}, "Dim": {Dim, th.Dim}, "OK": {OK, th.OK}, "Accent": {Accent, th.Accent},
		"Ref": {Ref, th.Ref}, "Critical": {Critical, th.Crit}, "Tick": {Tick, th.Tick}, "Strong": {Strong, th.Strong},
	}
	for name, tok := range tokens {
		if c := tuitest.Cells(tok.style.Render("x")); len(c) != 1 || c[0].FG != tok.fg || c[0].BG != th.BG {
			t.Errorf("%s under sensei = %+v, want fg %s bg %s", name, c, tok.fg, th.BG)
		}
	}
	if !Strong.GetBold() {
		t.Error("Strong lost its bold")
	}

	useDefault()
	if c := tuitest.Cells(Text.Render("x")); len(c) != 1 || c[0].FG != "#D9DCE4" || c[0].BG != "#1E2331" {
		t.Errorf("Text.Render after restoring the default = %+v", c)
	}
	if Current().Name != DefaultName {
		t.Errorf("Current() = %q, want %s", Current().Name, DefaultName)
	}
}

func TestDefaultThemeActiveAtLoad(t *testing.T) {
	if Current().Name != DefaultName {
		t.Fatalf("Current() = %q, want %s", Current().Name, DefaultName)
	}
	if c := tuitest.Cells(Dim.Render("x")); len(c) != 1 || c[0].FG != "#838A9E" || c[0].BG != "#1E2331" {
		t.Errorf("Dim = %+v", c)
	}
}

func TestWithRestores(t *testing.T) {
	t.Cleanup(useDefault)
	g, _ := ByName("gemini")
	var inside string
	out := With(g, func() string {
		inside = Current().Name
		return Text.Render("x")
	})
	if inside != "gemini" {
		t.Errorf("inside With: Current() = %q, want gemini", inside)
	}
	if c := tuitest.Cells(out); len(c) != 1 || c[0].BG != g.BG {
		t.Errorf("With output = %+v, want bg %s", c, g.BG)
	}
	if Current().Name != DefaultName {
		t.Errorf("after With: Current() = %q, want %s", Current().Name, DefaultName)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("With swallowed the panic")
			}
		}()
		With(g, func() string { panic("boom") })
	}()
	if Current().Name != DefaultName {
		t.Errorf("after a panic in With: Current() = %q, want %s", Current().Name, DefaultName)
	}
	if c := tuitest.Cells(Text.Render("x")); len(c) != 1 || c[0].BG != "#1E2331" {
		t.Errorf("styles not restored after a panic: %+v", c)
	}
}

func TestBrandGradient(t *testing.T) {
	g := useTheme(t, "gemini")
	cells := tuitest.Cells(Brand("pr-review"))
	if got := tuitest.Plain(Brand("pr-review")); got != "pr-review" {
		t.Fatalf("Brand text = %q", got)
	}
	colors := map[string]bool{}
	for i, c := range cells {
		want := GradientAt(g.Gradient, float64(i)/8, g.BG, 4.5)
		if c.FG != want || c.BG != g.BG {
			t.Errorf("char %d %q: fg %s bg %s, want fg %s bg %s", i, c.Text, c.FG, c.BG, want, g.BG)
		}
		colors[c.FG] = true
	}
	if len(colors) != 9 {
		t.Errorf("gemini brand has %d distinct colors, want 9: %+v", len(colors), cells)
	}

	codex := useTheme(t, "codex")
	if got, want := Brand("pr-review"), Strong.Render("pr-review"); got != want {
		t.Errorf("codex Brand = %q, want Strong %q", got, want)
	}
	for _, c := range tuitest.Cells(Brand("pr-review")) {
		if c.FG != codex.Strong || c.BG != codex.BG {
			t.Errorf("codex brand cell %+v, want fg %s bg %s", c, codex.Strong, codex.BG)
		}
	}
}

func TestMarker(t *testing.T) {
	g := useTheme(t, "gemini")
	if c := tuitest.Cells(Marker()); len(c) != 1 || c[0].Text != "▌" || c[0].FG != Lerp(g.Gradient, 0.5) || c[0].BG != g.BG {
		t.Errorf("gemini marker = %+v, want fg %s (middle of the gradient)", c, Lerp(g.Gradient, 0.5))
	}
	def := useTheme(t, DefaultName)
	if c := tuitest.Cells(Marker()); len(c) != 1 || c[0].Text != "▌" || c[0].FG != def.Accent || c[0].BG != def.BG {
		t.Errorf("default marker = %+v, want fg %s", c, def.Accent)
	}
}

func TestSeverityLabelCritico(t *testing.T) {
	codex := useTheme(t, "codex")
	label := SeverityLabel(finding.SeverityCritico)
	if got := tuitest.Plain(label); got != " Crítico " {
		t.Errorf("label text = %q", got)
	}
	for _, c := range tuitest.Cells(label) {
		if c.FG != codex.Label || c.BG != "#E5534B" || c.Reverse {
			t.Errorf("label cell %+v, want fg %s on bg #E5534B without reverse", c, codex.Label)
		}
	}
}
