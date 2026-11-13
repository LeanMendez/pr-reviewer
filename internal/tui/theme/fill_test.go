package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

func TestFillEveryCellHasBackground(t *testing.T) {
	th := useTheme(t, "gemini")
	content := strings.Join([]string{
		// styled spans with unstyled text between them
		Strong.Render("pr-review") + "   sin estilo " + Dim.Render("atenuado"),
		// resets in the middle of the line, both spellings, and a default
		// background; longer than the width
		"\x1b[1mnegrita\x1b[m medio \x1b[0mcero \x1b[38;2;1;2;3mcolor\x1b[49m fondo por defecto y más",
		"",
		// wide runes that cross the right edge
		Text.Render("x") + strings.Repeat("漢", 25),
	}, "\n") + "\n"

	got := Fill(content, 40, 6)
	lines := strings.Split(got, "\n")
	if len(lines) != 6 {
		t.Fatalf("Fill gave %d lines, want 6:\n%q", len(lines), got)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 40 {
			t.Errorf("line %d is %d cells wide, want 40: %q", i, w, l)
		}
		cells := tuitest.Cells(l)
		w := 0
		for _, c := range cells {
			w += c.Width
			if c.BG != th.BG || c.Reverse {
				t.Errorf("line %d cell %q: bg %q reverse %v, want bg %s", i, c.Text, c.BG, c.Reverse, th.BG)
			}
		}
		if w != 40 {
			t.Errorf("line %d has %d cells, want 40", i, w)
		}
	}
	for i, want := range []string{
		"pr-review   sin estilo atenuado",
		"negrita medio cero color fondo por defec",
		"",
		"x" + strings.Repeat("漢", 19),
	} {
		if p := strings.TrimRight(tuitest.Plain(lines[i]), " "); p != strings.TrimRight(want, " ") {
			t.Errorf("line %d text = %q, want %q", i, p, want)
		}
	}
}

func TestFillKeepsExplicitBackgrounds(t *testing.T) {
	th := useTheme(t, "codex")
	l := Fill(SeverityLabel(finding.SeverityCritico)+" después", 20, 1)
	for _, c := range tuitest.Cells(l)[:9] {
		if c.BG != th.Crit {
			t.Errorf("label cell %q lost its background: %+v", c.Text, c)
		}
	}
	for _, c := range tuitest.Cells(l)[9:] {
		if c.BG != th.BG {
			t.Errorf("cell %q after the label: bg %q, want %s", c.Text, c.BG, th.BG)
		}
	}
}

func TestFillTrailingNewlineAndUnknownSize(t *testing.T) {
	if got := strings.Count(Fill("a\n", 5, 1), "\n"); got != 0 {
		t.Errorf("trailing newline counted as a line: %d newlines", got)
	}
	if got := strings.Count(Fill("a\nb\nc", 5, 2), "\n"); got != 2 {
		t.Errorf("Fill cut lines beyond the height: %d newlines, want 2", got)
	}
	got := Fill("abc\nd", 0, 0)
	if p := tuitest.Plain(got); p != "abc\nd" {
		t.Errorf("Fill without a size changed the text: %q", p)
	}
	for _, c := range tuitest.Cells(strings.ReplaceAll(got, "\n", "")) {
		if c.BG != Current().BG {
			t.Errorf("cell %q without a size: bg %q", c.Text, c.BG)
		}
	}
}
