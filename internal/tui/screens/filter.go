package screens

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// filterInput is the "/" text filter of the list and the repo picker:
// typed while active, kept after enter, cleared with esc.
type filterInput struct {
	text   string
	active bool
}

// start begins typing the filter.
func (f *filterInput) start() { f.active = true }

// key handles a key while the filter is being typed. It returns how far
// the cursor of the filtered rows moves (up and down still move it).
func (f *filterInput) key(msg tea.KeyPressMsg) (move int) {
	switch msg.String() {
	case "enter":
		f.active = false
	case "esc":
		f.active, f.text = false, ""
	case "backspace":
		if r := []rune(f.text); len(r) > 0 {
			f.text = string(r[:len(r)-1])
		}
	case "up":
		return -1
	case "down":
		return 1
	default:
		if msg.Text != "" {
			f.text += clean(msg.Text)
		}
	}
	return 0
}

// matches reports whether s contains the filter, ignoring case.
func (f filterInput) matches(s string) bool {
	return strings.Contains(strings.ToLower(clean(s)), strings.ToLower(f.text))
}

// line is the filter as typed or applied; empty when there is none.
func (f filterInput) line() string {
	if f.active {
		return theme.Strong.Render(" /") + theme.Text.Render(" "+f.text) + theme.Accent.Render("▏")
	}
	if f.text != "" {
		return theme.Dim.Render(" filtro: ") + theme.Text.Render(f.text)
	}
	return ""
}
