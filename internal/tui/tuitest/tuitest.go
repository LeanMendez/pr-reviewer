// Package tuitest has helpers for testing Bubble Tea screens.
package tuitest

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Key builds a Bubble Tea key press: up, down, left, right, enter, esc,
// backspace, ctrl+c, or a single letter.
func Key(name string) tea.Msg {
	switch name {
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
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

// Cell is one visible grapheme of a rendered line with the SGR state in
// effect on it. Colors are "#RRGGBB", "" for the terminal default, or "?"
// for a palette color.
type Cell struct {
	Text    string
	Width   int
	FG, BG  string
	Reverse bool
}

// Cells walks a rendered line, tracking the SGR colors and reverse video,
// and returns its visible graphemes.
func Cells(line string) []Cell {
	var cells []Cell
	var st Cell
	p := ansi.NewParser()
	var state byte
	for len(line) > 0 {
		seq, width, n, next := ansi.DecodeSequence(line, state, p)
		state, line = next, line[n:]
		switch {
		case width > 0:
			st.Text, st.Width = seq, width
			cells = append(cells, st)
		case strings.HasPrefix(seq, "\x1b[") && ansi.Cmd(p.Command()) == 'm':
			st = sgr(p.Params(), st)
		}
	}
	return cells
}

// sgr applies an SGR sequence to the colors and reverse flag of st.
func sgr(params ansi.Params, st Cell) Cell {
	if len(params) == 0 {
		return Cell{}
	}
	for i := 0; i < len(params); i++ {
		switch v := params[i].Param(0); {
		case v == 0:
			st = Cell{}
		case v == 7:
			st.Reverse = true
		case v == 27:
			st.Reverse = false
		case v == 39:
			st.FG = ""
		case v == 49:
			st.BG = ""
		case v == 38 || v == 48 || v == 58:
			var c color.Color
			n := ansi.ReadStyleColor(params[i:], &c)
			hex := "?"
			if rgb, ok := c.(color.RGBA); ok {
				hex = fmt.Sprintf("#%02X%02X%02X", rgb.R, rgb.G, rgb.B)
			}
			switch v {
			case 38:
				st.FG = hex
			case 48:
				st.BG = hex
			}
			i += max(n, 1) - 1
		case v >= 30 && v <= 37, v >= 90 && v <= 97:
			st.FG = "?"
		case v >= 40 && v <= 47, v >= 100 && v <= 107:
			st.BG = "?"
		}
	}
	return st
}
