// Package theme defines the color tokens and styles shared by every screen.
//
// The active theme lives in package variables: Use reassigns the styles, so
// Use and With must only run on the Bubble Tea goroutine (Update and View
// both run there) or in tests, never while another goroutine renders. A test
// that switches the theme restores the default with t.Cleanup, so package
// tests stay independent of their order.
package theme

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/LeanMendez/pr-review/internal/finding"
)

// Style tokens of the active theme. Every one carries the theme background.
// Critical's color is reserved for the background of the "Crítico" label.
var (
	Text, Dim, OK, Accent, Ref, Critical, Tick lipgloss.Style
	Strong                                     lipgloss.Style // bold
)

var (
	current Theme
	label   lipgloss.Style // "Crítico": Label text on a Crit background
	bgSeq   string         // SGR sequence that sets the theme background
)

func init() {
	t, _ := ByName(DefaultName)
	Use(t)
}

// Use makes t the active theme.
func Use(t Theme) {
	bg := lipgloss.Color(t.BG)
	style := func(fg string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(bg)
	}
	Text, Dim, OK, Accent = style(t.Text), style(t.Dim), style(t.OK), style(t.Accent)
	Ref, Critical, Tick = style(t.Ref), style(t.Crit), style(t.Tick)
	Strong = style(t.Strong).Bold(true)
	label = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Label)).Background(lipgloss.Color(t.Crit))
	r, g, b := parseHex(t.BG)
	bgSeq = fmt.Sprintf("\x1b[48;2;%d;%d;%dm", int(r), int(g), int(b))
	current = t
}

// Current returns the active theme.
func Current() Theme { return current }

// With renders fn under t and then restores the previous theme, even if fn
// panics.
func With(t Theme, fn func() string) string {
	prev := current
	defer Use(prev)
	Use(t)
	return fn()
}

// Brand renders the program name: in Strong, or one gradient color per
// character if the theme has a gradient.
func Brand(s string) string {
	g := current.Gradient
	if len(g) == 0 {
		return Strong.Render(s)
	}
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		x := 0.0
		if len(runes) > 1 {
			x = float64(i) / float64(len(runes)-1)
		}
		c := GradientAt(g, x, current.BG, 4.5)
		b.WriteString(Strong.Foreground(lipgloss.Color(c)).Render(string(r)))
	}
	return b.String()
}

// Marker renders the selection marker: in Accent, or in the middle color of
// the gradient if the theme has one.
func Marker() string {
	g := current.Gradient
	if len(g) == 0 {
		return Accent.Render("▌")
	}
	return Accent.Foreground(lipgloss.Color(GradientAt(g, 0.5, current.BG, 4.5))).Render("▌")
}

// SeverityLabel renders the label for a finding severity.
func SeverityLabel(s finding.Severity) string {
	switch s {
	case finding.SeverityCritico:
		return label.Render(" Crítico ")
	case finding.SeverityAlto:
		return Accent.Render("Alto")
	case finding.SeverityMedio:
		return Strong.Render("Medio")
	default:
		return Dim.Render("Bajo")
	}
}
