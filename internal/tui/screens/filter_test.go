package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

func filterKey(f *filterInput, k string) int {
	return f.key(tuitest.Key(k).(tea.KeyPressMsg))
}

func TestFilterInput(t *testing.T) {
	var f filterInput
	if f.line() != "" {
		t.Errorf("idle filter renders %q", f.line())
	}
	f.start()
	for _, k := range []string{"p", "a", "g"} {
		if move := filterKey(&f, k); move != 0 {
			t.Errorf("%s moved the cursor by %d", k, move)
		}
	}
	filterKey(&f, "backspace")
	if f.text != "pa" || !f.active {
		t.Fatalf("after typing: %+v", f)
	}
	if got := tuitest.Plain(f.line()); got != " / pa▏" {
		t.Errorf("typing line = %q", got)
	}
	if filterKey(&f, "up") != -1 || filterKey(&f, "down") != 1 {
		t.Error("up and down move the cursor while typing")
	}
	filterKey(&f, "enter")
	if f.active || f.text != "pa" {
		t.Errorf("enter applies the filter: %+v", f)
	}
	if got := tuitest.Plain(f.line()); got != " filtro: pa" {
		t.Errorf("applied line = %q", got)
	}
	if !f.matches("Pagos") || f.matches("stock") {
		t.Error("matches is a case-insensitive substring test")
	}
	f.start()
	filterKey(&f, "esc")
	if f.active || f.text != "" || !f.matches("lo que sea") {
		t.Errorf("esc clears the filter: %+v", f)
	}
	f.start()
	f.key(tea.KeyPressMsg{Code: 'a', Text: "a\x07"})
	if f.text != "a" || strings.ContainsRune(f.text, '\x07') {
		t.Errorf("control characters typed into the filter: %q", f.text)
	}
}
