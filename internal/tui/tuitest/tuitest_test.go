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
