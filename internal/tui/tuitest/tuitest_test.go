package tuitest

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestKeyBackspace(t *testing.T) {
	k, ok := Key("backspace").(tea.KeyPressMsg)
	if !ok || k.Code != tea.KeyBackspace {
		t.Errorf("Key(backspace) = %#v", Key("backspace"))
	}
}

func TestKeyCtrlC(t *testing.T) {
	if k, ok := Key("ctrl+c").(tea.KeyPressMsg); !ok || k.String() != "ctrl+c" {
		t.Errorf("Key(ctrl+c) = %#v", Key("ctrl+c"))
	}
}

func TestCells(t *testing.T) {
	line := "a\x1b[38;2;0;0;0;48;2;1;2;3mb日\x1b[39mc\x1b[0;1;7md\x1b[27;48;5;4me\x1b[49mf\x1b[42mg\x1b[m h"
	want := []Cell{
		{"a", 1, "", "", false},
		{"b", 1, "#000000", "#010203", false},
		{"日", 2, "#000000", "#010203", false},
		{"c", 1, "", "#010203", false},
		{"d", 1, "", "", true},
		{"e", 1, "", "?", false},
		{"f", 1, "", "", false},
		{"g", 1, "", "?", false},
		{" ", 1, "", "", false},
		{"h", 1, "", "", false},
	}
	if got := Cells(line); !reflect.DeepEqual(got, want) {
		t.Errorf("Cells =\n %v\nwant\n %v", got, want)
	}
}
