package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

func newMode() ModeModel {
	m := NewMode(mkRow(477, "Migrar facturas", "Lucía G.", 300, 0), config.Defaults())
	n, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return n.(ModeModel)
}

func TestModeRender(t *testing.T) {
	cfg := config.Defaults()
	out := tuitest.Plain(newMode().render(100, 24))
	for _, want := range []string{
		"#477", "Migrar facturas",
		"ligero", "exhaustivo", "judgment",
		"unos 4 agentes", "unos 9 agentes", "unos 11 agentes",
		"Chequeos sin IA", "Jueces",
		"jueces: " + string(cfg.AssignmentFor(config.RoleJuezA).Model) + " y " + string(cfg.AssignmentFor(config.RoleJuezB).Model),
		"enter elegir", "esc volver",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "sugerido") != 1 {
		t.Errorf("want one 'sugerido':\n%s", out)
	}
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		if strings.Contains(l, "exhaustivo") && strings.Contains(l, "sugerido") && strings.Contains(l, "▌") {
			return
		}
	}
	t.Errorf("suggested mode not preselected:\n%s", out)
}

func TestModeWidth(t *testing.T) {
	for _, w := range []int{40, 60, 100} {
		checkWidth(t, newMode().render(w, 24), w)
	}
}

func TestModeKeys(t *testing.T) {
	run := func(m ModeModel, keys ...string) tea.Msg {
		var out tea.Msg
		var model tea.Model = m
		for _, k := range keys {
			var cmd tea.Cmd
			model, cmd = model.Update(tuitest.Key(k))
			out = nil
			if cmd != nil {
				out = cmd()
			}
		}
		return out
	}
	got, ok := run(newMode(), "enter").(StartReviewMsg)
	if !ok || got.Mode != pipeline.ModeExhaustivo || got.Row.Ctx.PullRequest.ID != 477 {
		t.Errorf("enter on suggested = %+v", got)
	}
	got, ok = run(newMode(), "down", "enter").(StartReviewMsg)
	if !ok || got.Mode != pipeline.ModeJudgment {
		t.Errorf("down+enter = %+v", got)
	}
	got, ok = run(newMode(), "up", "up", "up", "enter").(StartReviewMsg)
	if !ok || got.Mode != pipeline.ModeLigero {
		t.Errorf("up x3 + enter = %+v", got)
	}
	if _, ok := run(newMode(), "esc").(BackMsg); !ok {
		t.Error("esc did not emit BackMsg")
	}
}
