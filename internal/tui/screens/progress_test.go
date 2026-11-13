package screens

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/tui/listdata"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newProgress(mode pipeline.Mode, cancel context.CancelFunc) (ProgressModel, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)}
	row := listdata.Row{}
	row.Ctx.PullRequest = prb.PR{ID: 477, Title: "Migrar facturas"}
	return NewProgress(row, mode, make(chan pipeline.Event), cancel).WithClock(clk.now), clk
}

func feed(m ProgressModel, evs ...pipeline.Event) ProgressModel {
	for _, e := range evs {
		next, _ := m.Update(EventMsg(e))
		m = next.(ProgressModel)
	}
	return m
}

func press(m ProgressModel, k string) (ProgressModel, tea.Cmd) {
	next, cmd := m.Update(tuitest.Key(k))
	return next.(ProgressModel), cmd
}

func plain(m ProgressModel) string { return tuitest.Plain(m.render(120, 0)) }

func lineWith(s, sub string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

func TestProgressStages(t *testing.T) {
	m, _ := newProgress(pipeline.ModeExhaustivo, nil)
	m = feed(m,
		pipeline.Event{Kind: pipeline.StageStarted, Stage: 1}, pipeline.Event{Kind: pipeline.StageDone, Stage: 1},
		pipeline.Event{Kind: pipeline.StageStarted, Stage: 2}, pipeline.Event{Kind: pipeline.StageDone, Stage: 2},
		pipeline.Event{Kind: pipeline.StageStarted, Stage: 3}, pipeline.Event{Kind: pipeline.StageDone, Stage: 3},
		pipeline.Event{Kind: pipeline.StageStarted, Stage: 4})
	out := plain(m)
	for _, c := range []struct {
		stage pipeline.Stage
		glyph string
	}{
		{1, "✓"}, {2, "✓"}, {3, "✓"}, {4, "●"}, {5, "·"}, {7, "·"},
	} {
		if l := lineWith(out, c.stage.Name()); !strings.Contains(l, c.glyph+" ") {
			t.Errorf("stage %d line %q lacks %s", c.stage, l, c.glyph)
		}
	}
	if !strings.Contains(out, "Va 3 de 6 etapas") {
		t.Errorf("progress count missing:\n%s", out)
	}
	if strings.Contains(out, "Jueces") {
		t.Errorf("exhaustivo must not list the judges:\n%s", out)
	}
	mj, _ := newProgress(pipeline.ModeJudgment, nil)
	if !strings.Contains(plain(mj), "Jueces") || !strings.Contains(plain(mj), "de 7 etapas") {
		t.Errorf("judgment must list the judges:\n%s", plain(mj))
	}
}

func TestProgressAgents(t *testing.T) {
	m, clk := newProgress(pipeline.ModeLigero, nil)
	m = feed(m,
		pipeline.Event{Kind: pipeline.StageStarted, Stage: pipeline.StageSpecialists},
		pipeline.Event{Kind: pipeline.AgentStarted, Agent: "bugs", Role: config.RoleBugs, Model: config.ModelOpus},
		pipeline.Event{Kind: pipeline.AgentStarted, Agent: "seguridad", Role: config.RoleSeguridad, Model: config.ModelSonnet})
	clk.t = clk.t.Add(74 * time.Second)
	m = feed(m, pipeline.Event{Kind: pipeline.AgentActivity, Agent: "bugs", Text: "leyendo src/a.go"})
	l := lineWith(plain(m), "bugs y lógica")
	for _, w := range []string{"opus", "leyendo src/a.go", "1m 14s"} {
		if !strings.Contains(l, w) {
			t.Errorf("running row %q lacks %q", l, w)
		}
	}
	m = feed(m,
		pipeline.Event{Kind: pipeline.AgentDone, Agent: "bugs", Candidates: 2},
		pipeline.Event{Kind: pipeline.AgentFailed, Agent: "seguridad", Err: errors.New("se cortó")})
	clk.t = clk.t.Add(time.Minute) // finished agents keep their time
	out := plain(m)
	l = lineWith(out, "bugs y lógica")
	for _, w := range []string{"terminó", "2 candidatos", "1m 14s"} {
		if !strings.Contains(l, w) {
			t.Errorf("done row %q lacks %q", l, w)
		}
	}
	if !strings.Contains(lineWith(out, "seguridad"), "falló: se cortó") {
		t.Errorf("failed row missing:\n%s", out)
	}
	if !strings.Contains(m.render(120, 0), strings.TrimSuffix(theme.Accent.Render("x"), "x\x1b[m")+"falló: se cortó") {
		t.Error("failure must use the Accent color")
	}
	if strings.Contains(m.render(120, 0), "\x1b[38;2;238;127;122m") {
		t.Error("no red on this screen")
	}
	if !strings.Contains(out, "2 candidatos hasta ahora.") {
		t.Errorf("candidate total missing:\n%s", out)
	}
	// Agents of a finished stage are not listed.
	m = feed(m, pipeline.Event{Kind: pipeline.StageDone, Stage: pipeline.StageSpecialists})
	if strings.Contains(plain(m), "bugs y lógica") {
		t.Error("agents must hide once their stage is done")
	}
}

func TestProgressSanitizesAgentText(t *testing.T) {
	m, _ := newProgress(pipeline.ModeLigero, nil)
	m = feed(m,
		pipeline.Event{Kind: pipeline.StageStarted, Stage: pipeline.StageSpecialists},
		pipeline.Event{Kind: pipeline.AgentStarted, Agent: "bugs", Role: config.RoleBugs, Model: config.ModelOpus},
		pipeline.Event{Kind: pipeline.AgentActivity, Agent: "bugs", Text: "leyendo \x1b[31mrojo\x1b]0;x\x07\nlargo " + strings.Repeat("x", 300)})
	for _, l := range strings.Split(m.render(80, 0), "\n") {
		if strings.ContainsAny(l, "\a\r") || strings.Count(l, "\x1b") != strings.Count(l, "\x1b[") {
			t.Errorf("control chars leaked: %q", l)
		}
	}
	out := tuitest.Plain(m.render(80, 0))
	if w := longest(out); w > 80 {
		t.Errorf("line of %d columns exceeds 80", w)
	}
}

func longest(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		n = max(n, len([]rune(l)))
	}
	return n
}

func TestProgressHidesFindings(t *testing.T) {
	m, _ := newProgress(pipeline.ModeLigero, nil)
	m = feed(m,
		pipeline.Event{Kind: pipeline.StageStarted, Stage: pipeline.StageSpecialists},
		pipeline.Event{Kind: pipeline.AgentStarted, Agent: "bugs", Role: config.RoleBugs, Model: config.ModelOpus},
		pipeline.Event{Kind: pipeline.AgentDone, Agent: "bugs", Candidates: 6})
	out := plain(m)
	if !strings.Contains(out, "6 candidatos hasta ahora.") || !strings.Contains(out, "No se muestran hasta que pasen la validación") {
		t.Errorf("hint missing:\n%s", out)
	}
	m = feed(m, pipeline.Event{Kind: pipeline.StageDone, Stage: pipeline.StageSpecialists},
		pipeline.Event{Kind: pipeline.StageStarted, Stage: pipeline.StageValidation, Candidates: 4})
	if !strings.Contains(plain(m), "4 candidatos hasta ahora.") {
		t.Errorf("validation count should replace the total:\n%s", plain(m))
	}
}

func TestProgressKeys(t *testing.T) {
	cancelled := 0
	m, _ := newProgress(pipeline.ModeLigero, func() { cancelled++ })
	if _, cmd := press(m, "s"); cmd == nil || cmd().(BackgroundMsg).PR != 477 {
		t.Error("s must emit BackgroundMsg{PR: 477}")
	}
	m, _ = press(m, "c")
	m, _ = press(m, "c")
	if cancelled != 1 || !strings.Contains(plain(m), "Cancelando…") {
		t.Errorf("c: cancelled=%d\n%s", cancelled, plain(m))
	}

	m, _ = newProgress(pipeline.ModeLigero, nil)
	m = feed(m, pipeline.Event{Kind: pipeline.StageStarted, Stage: pipeline.StageSpecialists},
		pipeline.Event{Kind: pipeline.AgentStarted, Agent: "bugs", Role: config.RoleBugs, Model: config.ModelOpus})
	for i := 1; i <= 7; i++ {
		m = feed(m, pipeline.Event{Kind: pipeline.AgentActivity, Agent: "bugs", Text: "paso " + string(rune('0'+i))})
	}
	if strings.Contains(plain(m), "paso 3") {
		t.Error("log hidden by default")
	}
	m, _ = press(m, "l")
	out := plain(m)
	if strings.Contains(out, "paso 2") || !strings.Contains(out, "paso 3") || !strings.Contains(out, "paso 7") {
		t.Errorf("log must show the last 5 activities:\n%s", out)
	}
	m, _ = press(m, "l")
	if strings.Contains(plain(m), "paso 3") {
		t.Error("l again hides the log")
	}
}

func TestProgressDone(t *testing.T) {
	m, _ := newProgress(pipeline.ModeLigero, nil)
	res := pipeline.Result{Phrase: "ok"}
	next, cmd := m.Update(ReviewDoneMsg{Result: res})
	if cmd == nil || cmd().(OpenResultMsg).Result.Phrase != "ok" {
		t.Error("success must emit OpenResultMsg")
	}
	_ = next
	failed, _ := m.Update(ReviewDoneMsg{Err: errors.New("claude no respondió")})
	out := tuitest.Plain(failed.(ProgressModel).render(120, 0))
	if !strings.Contains(out, "claude no respondió") || !strings.Contains(out, "q volver a la lista") {
		t.Errorf("error screen:\n%s", out)
	}
	_, cmd = failed.(ProgressModel).key(tuitest.Key("q").(tea.KeyPressMsg))
	if _, ok := cmd().(BackMsg); !ok {
		t.Error("q must go back after an error")
	}
	canceled, _ := m.Update(ReviewDoneMsg{Err: context.Canceled})
	if !strings.Contains(tuitest.Plain(canceled.(ProgressModel).render(120, 0)), "se canceló") {
		t.Error("cancellation gets a friendly message")
	}
}

func TestProgressTickOnlyRerenders(t *testing.T) {
	m, clk := newProgress(pipeline.ModeLigero, nil)
	m = feed(m, pipeline.Event{Kind: pipeline.StageStarted, Stage: pipeline.StageSpecialists})
	clk.t = clk.t.Add(5 * time.Second)
	next, cmd := m.Update(tickMsg{})
	if cmd == nil || !strings.Contains(plain(next.(ProgressModel)), "5 s") {
		t.Error("tick must re-arm and the elapsed time follows the clock")
	}
}

func TestRoleLabel(t *testing.T) {
	if n, d := roleLabel(config.RoleBugs); n != "Bugs y lógica" || d != "busca errores de comportamiento" {
		t.Errorf("got %q / %q", n, d)
	}
	for _, r := range []config.Role{config.RoleSuperior, config.RoleResumen, config.RoleSeguridad, config.RoleReglas,
		config.RoleTests, config.RoleValidadorBugs, config.RoleValidadorReglas, config.RoleJuezA, config.RoleJuezB} {
		if n, d := roleLabel(r); n == string(r) || d == "" {
			t.Errorf("role %s has no label", r)
		}
	}
}
