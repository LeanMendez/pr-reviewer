package components

import (
	"strings"

	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// Key is one entry of the key-help footer.
type Key struct{ Keys, Desc string }

// KeyHelp renders the footer: strong key, dim description, four spaces apart.
func KeyHelp(ks []Key) string {
	parts := make([]string, len(ks))
	for i, k := range ks {
		parts[i] = theme.Strong.Render(k.Keys) + theme.Dim.Render(" "+k.Desc)
	}
	return strings.Join(parts, "    ")
}
