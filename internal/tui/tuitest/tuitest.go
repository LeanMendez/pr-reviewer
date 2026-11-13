// Package tuitest has helpers for testing Bubble Tea screens.
package tuitest

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Key builds a Bubble Tea key press: up, down, left, right, enter, esc, or a
// single letter. "backspace" is also accepted.
func Key(name string) tea.Msg {
	switch name {
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	r := []rune(name)
	if len(r) != 1 {
		panic("tuitest.Key: unknown key " + name)
	}
	return tea.KeyPressMsg{Code: r[0], Text: name}
}

// Plain strips ANSI escape sequences.
func Plain(s string) string { return ansi.Strip(s) }
