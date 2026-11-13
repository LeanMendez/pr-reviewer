// Package components holds the small widgets shared by the screens.
package components

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

const (
	barCells      = 8
	overflowCells = 4
)

var partials = []rune("▏▎▍▌▋▊▉")

// SizeBar draws a PR size bar of fixed visible width 14: 8 cells up to the
// threshold tick, then 5 columns for the overflow (4 cells plus a » marker).
func SizeBar(lines, threshold int) string {
	if threshold < 1 {
		threshold = 1
	}
	if lines < 0 {
		lines = 0
	}
	cell := float64(threshold) / barCells

	base := lines
	if base > threshold {
		base = threshold
	}
	filled, partial := cells(float64(base)/cell, barCells)
	var b strings.Builder
	b.WriteString(paintFilled(filled + partial))
	empty := barCells - len([]rune(filled)) - len([]rune(partial))
	b.WriteString(theme.Tick.Render(strings.Repeat("·", empty) + "│"))

	over := 0.0
	if lines > threshold {
		over = float64(lines-threshold) / cell
	}
	var tail string
	switch {
	case over > overflowCells:
		tail = strings.Repeat("█", overflowCells) + "»"
	case over > 0:
		f, p := cells(over, overflowCells)
		tail = f + p
	}
	pad := overflowCells + 1 - len([]rune(tail))
	b.WriteString(theme.Accent.Render(tail))
	b.WriteString(strings.Repeat(" ", pad))
	return b.String()
}

// paintFilled colors the filled cells up to the tick: in Dim, or, if the
// theme has a gradient, each cell with the gradient color of its position
// along the 8.
func paintFilled(s string) string {
	th := theme.Current()
	if len(th.Gradient) == 0 {
		return theme.Dim.Render(s)
	}
	var b strings.Builder
	for i, r := range []rune(s) {
		c := theme.GradientAt(th.Gradient, float64(i)/(barCells-1), th.BG, 3)
		b.WriteString(theme.Dim.Foreground(lipgloss.Color(c)).Render(string(r)))
	}
	return b.String()
}

// cells splits a size in cells into full blocks and an optional partial block.
func cells(n float64, max int) (full, partial string) {
	whole := int(n)
	if whole >= max {
		return strings.Repeat("█", max), ""
	}
	if i := int((n-float64(whole))*8+0.5) - 1; i >= 0 {
		if i > len(partials)-1 {
			i = len(partials) - 1
		}
		partial = string(partials[i])
	}
	return strings.Repeat("█", whole), partial
}
