// Package theme defines the color tokens and styles shared by every screen.
package theme

import (
	"charm.land/lipgloss/v2"

	"github.com/LeanMendez/pr-review/internal/finding"
)

// Color tokens (spec 7.4). Critical is reserved for critical-finding labels.
var (
	Text     = lipgloss.NewStyle().Foreground(lipgloss.Color("#D9DCE4"))
	Dim      = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D859A"))
	OK       = lipgloss.NewStyle().Foreground(lipgloss.Color("#86C9A8"))
	Accent   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E7B45A"))
	Ref      = lipgloss.NewStyle().Foreground(lipgloss.Color("#A99DD8"))
	Critical = lipgloss.NewStyle().Foreground(lipgloss.Color("#EE7F7A"))
	Tick     = lipgloss.NewStyle().Foreground(lipgloss.Color("#4E5568"))
	Strong   = lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F8")).Bold(true)
)

// SeverityLabel renders the label for a finding severity.
func SeverityLabel(s finding.Severity) string {
	switch s {
	case finding.SeverityCritico:
		return Critical.Reverse(true).Render(" Crítico ")
	case finding.SeverityAlto:
		return Accent.Render("Alto")
	case finding.SeverityMedio:
		return Strong.Render("Medio")
	default:
		return Dim.Render("Bajo")
	}
}
