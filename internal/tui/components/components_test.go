package components

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

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
