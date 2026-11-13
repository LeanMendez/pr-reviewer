package tuitest

import (
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
