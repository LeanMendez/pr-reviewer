package screens

import (
	"context"
	"errors"
	"fmt"
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
		pipeline.Event{Kind: pipeline.AgentStarted, Stage: pipeline.StageSpecialists, Agent: "bugs", Role: config.RoleBugs, Model: config.ModelOpus},
		pipeline.Event{Kind: pipeline.AgentStarted, Stage: pipeline.StageSpecialists, Agent: "seguridad", Role: config.RoleSeguridad, Model: config.ModelSonnet})
	clk.t = clk.t.Add(74 * time.Second)
	m = feed(m, pipeline.Event{Kind: pipeline.AgentActivity, Stage: pipeline.StageSpecialists, Agent: "bugs", Text: "leyendo src/a.go"})
	l := lineWith(plain(m), "bugs y lógica")
	for _, w := range []string{"opus", "leyendo src/a.go", "1m 14s"} {
		if !strings.Contains(l, w) {
			t.Errorf("running row %q lacks %q", l, w)
		}
	}
	m = feed(m,
		pipeline.Event{Kind: pipeline.AgentDone, Stage: pipeline.StageSpecialists, Agent: "bugs", Candidates: 2},
		pipeline.Event{Kind: pipeline.AgentFailed, Stage: pipeline.StageSpecialists, Agent: "seguridad", Err: errors.New("se cortó")})
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
	for _, l := range strings.Split(m.render(120, 0), "\n") {
		for _, c := range tuitest.Cells(l) {
			if red := theme.Current().Crit; c.FG == red || c.BG == red {
				t.Fatalf("no red on this screen: %+v in %q", c, tuitest.Plain(l))
			}
		}
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
		pipeline.Event{Kind: pipeline.AgentStarted, Stage: pipeline.StageSpecialists, Agent: "bugs", Role: config.RoleBugs, Model: config.ModelOpus},
		pipeline.Event{Kind: pipeline.AgentActivity, Stage: pipeline.StageSpecialists, Agent: "bugs", Text: "leyendo \x1b[31mrojo\x1b]0;x\x07\nlargo " + strings.Repeat("x", 300)})
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
		pipeline.Event{Kind: pipeline.AgentStarted, Stage: pipeline.StageSpecialists, Agent: "bugs", Role: config.RoleBugs, Model: config.ModelOpus},
		pipeline.Event{Kind: pipeline.AgentDone, Stage: pipeline.StageSpecialists, Agent: "bugs", Candidates: 6})
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
		pipeline.Event{Kind: pipeline.AgentStarted, Stage: pipeline.StageSpecialists, Agent: "bugs", Role: config.RoleBugs, Model: config.ModelOpus})
	for i := 1; i <= 7; i++ {
		m = feed(m, pipeline.Event{Kind: pipeline.AgentActivity, Stage: pipeline.StageSpecialists, Agent: "bugs", Text: "paso " + string(rune('0'+i))})
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

func validationWithAgents(n int) ProgressModel {
	m, _ := newProgress(pipeline.ModeJudgment, nil)
	m = feed(m,
		pipeline.Event{Kind: pipeline.StageStarted, Stage: 1}, pipeline.Event{Kind: pipeline.StageDone, Stage: 1},
		pipeline.Event{Kind: pipeline.StageStarted, Stage: pipeline.StageValidation, Candidates: n})
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("validador_bugs-bugs-%03d", i)
		m = feed(m, pipeline.Event{Kind: pipeline.AgentStarted, Stage: pipeline.StageValidation, Agent: name,
			Role: config.RoleValidadorBugs, Model: config.ModelOpus})
		if i%2 == 0 {
			m = feed(m, pipeline.Event{Kind: pipeline.AgentDone, Stage: pipeline.StageValidation, Agent: name})
		}
	}
	return m
}

func TestProgressFitsHeightKeepingStages(t *testing.T) {
	for _, log := range []bool{false, true} {
		m := validationWithAgents(20)
		if log {
			m, _ = press(m, "l")
		}
		for _, h := range []int{24, 16} {
			out := tuitest.Plain(m.render(120, h))
			lines := strings.Split(out, "\n")
			if len(lines) > h {
				t.Errorf("log=%v h=%d: %d lines exceed the height", log, h, len(lines))
			}
			must := []string{"pr-review", "#477"}
			for _, s := range pipeline.Stages(pipeline.ModeJudgment) {
				must = append(must, s.Name())
			}
			must = append(must, "candidatos hasta ahora", "cancelar", " más")
			for _, w := range must {
				if !strings.Contains(out, w) {
					t.Errorf("log=%v h=%d: %q missing:\n%s", log, h, w, out)
				}
			}
		}
	}
	// running agents win over finished ones when space is short
	out := plain(validationWithAgents(20))
	if !strings.Contains(tuitest.Plain(validationWithAgents(20).render(120, 24)), "bugs-001") {
		t.Errorf("running validator must be listed first:\n%s", out)
	}
}

func TestProgressCancelledShownForAnyErrorAfterC(t *testing.T) {
	m, _ := newProgress(pipeline.ModeLigero, func() {})
	m, _ = press(m, "c")
	next, _ := m.Update(ReviewDoneMsg{Err: errors.New("signal: killed")})
	out := tuitest.Plain(next.(ProgressModel).render(120, 0))
	if !strings.Contains(out, "La revisión se canceló.") || strings.Contains(out, "killed") {
		t.Errorf("got:\n%s", out)
	}
}

func TestProgressAgentStageFromEvent(t *testing.T) {
	m, _ := newProgress(pipeline.ModeLigero, nil)
	m = feed(m,
		pipeline.Event{Kind: pipeline.AgentStarted, Stage: pipeline.StageSpecialists, Agent: "bugs", Role: config.RoleBugs, Model: config.ModelOpus},
		pipeline.Event{Kind: pipeline.StageStarted, Stage: pipeline.StageSpecialists},
		pipeline.Event{Kind: pipeline.AgentDone, Stage: pipeline.StageSpecialists, Agent: "bugs", Candidates: 3})
	out := plain(m)
	if !strings.Contains(lineWith(out, "bugs y lógica"), "3 candidatos") || !strings.Contains(out, "3 candidatos hasta ahora.") {
		t.Errorf("early agent lost:\n%s", out)
	}
	// a validator's Candidates are not added to the total
	m = feed(m, pipeline.Event{Kind: pipeline.AgentDone, Stage: pipeline.StageValidation, Agent: "x", Candidates: 9})
	if strings.Contains(plain(m), "12 candidatos") {
		t.Error("only specialists add candidates")
	}
}

func TestProgressStageNumbersAreFixed(t *testing.T) {
	m, _ := newProgress(pipeline.ModeLigero, nil)
	out := plain(m)
	if l := lineWith(out, "Reporte"); !strings.HasPrefix(strings.TrimSpace(l), "7") {
		t.Errorf("Reporte must be 7: %q", l)
	}
	if l := lineWith(out, "Validación"); !strings.HasPrefix(strings.TrimSpace(l), "5") {
		t.Errorf("Validación must be 5: %q", l)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "6 ") {
			t.Errorf("ligero has no stage 6: %q", l)
		}
	}
	if !strings.Contains(out, "de 6 etapas") {
		t.Errorf("footer counts the mode's stages:\n%s", out)
	}
}

func TestProgressValidatorShowsCandidate(t *testing.T) {
	out := tuitest.Plain(validationWithAgents(3).render(120, 0))
	for _, w := range []string{"validador de bugs · bugs-000", "validador de bugs · bugs-002"} {
		if !strings.Contains(out, w) {
			t.Errorf("%q missing:\n%s", w, out)
		}
	}
}

// TestProgressWithoutChannel: the router reads the review's events and
// delivers them, so the screen must not wait on a channel of its own.
func TestProgressWithoutChannel(t *testing.T) {
	m := NewProgress(listdata.Row{}, pipeline.ModeLigero, nil, nil)
	next, cmd := m.Update(EventMsg(pipeline.Event{Kind: pipeline.StageStarted, Stage: pipeline.StagePrechecks}))
	if cmd != nil {
		t.Error("an event must not start waiting on a nil channel")
	}
	if out := plain(next.(ProgressModel)); !strings.Contains(out, "Va 0 de") {
		t.Errorf("event not applied:\n%s", out)
	}
}

func TestProgressAgentCountIsAnEstimate(t *testing.T) {
	m, _ := newProgress(pipeline.ModeLigero, nil)
	est := pipeline.EstimateAgents(pipeline.ModeLigero)
	if out := plain(m); !strings.Contains(out, fmt.Sprintf("Esta revisión usa unos %d agentes en total.", est)) {
		t.Errorf("the estimate must read as one:\n%s", out)
	}
	// more agents than estimated: the count follows the ones started
	for i := 0; i < est+2; i++ {
		m = feed(m, pipeline.Event{Kind: pipeline.AgentStarted, Stage: pipeline.StageSpecialists,
			Agent: fmt.Sprintf("bugs-%d", i+1), Role: config.RoleBugs, Model: config.ModelOpus})
	}
	if out := plain(m); !strings.Contains(out, fmt.Sprintf("Esta revisión usa unos %d agentes en total.", est+2)) {
		t.Errorf("more agents than estimated:\n%s", out)
	}
}

func TestProgressPrbHint(t *testing.T) {
	m, _ := newProgress(pipeline.ModeLigero, nil)
	n, _ := m.Update(ReviewDoneMsg{PR: 477, Err: &prb.Error{Code: "no_credentials", Message: "missing token"}})
	if out := plain(n.(ProgressModel)); !strings.Contains(out, "No hay sesión de Bitbucket. Corré prb auth login") {
		t.Errorf("a prb session error needs the same hint as the list:\n%s", out)
	}
	n, _ = m.Update(ReviewDoneMsg{PR: 477, Err: &prb.Error{Code: "rate_limited", Message: "demasiados pedidos"}})
	if out := plain(n.(ProgressModel)); !strings.Contains(out, "demasiados pedidos") {
		t.Errorf("other prb errors keep their text:\n%s", out)
	}
}

func TestProgressEmptyLogIsDim(t *testing.T) {
	m, _ := newProgress(pipeline.ModeLigero, nil)
	m = feed(m,
		pipeline.Event{Kind: pipeline.StageStarted, Stage: pipeline.StageSpecialists},
		pipeline.Event{Kind: pipeline.AgentStarted, Stage: pipeline.StageSpecialists, Agent: "bugs", Role: config.RoleBugs, Model: config.ModelOpus})
	m, _ = press(m, "l")
	line := lineWith(m.render(120, 0), "sin actividad registrada")
	if line == "" {
		t.Fatal("no empty-log line")
	}
	for _, c := range tuitest.Cells(line) {
		if strings.TrimSpace(c.Text) != "" && !strings.EqualFold(c.FG, theme.Current().Dim) {
			t.Fatalf("cell %q fg %s, want Dim %s", c.Text, c.FG, theme.Current().Dim)
		}
	}
}
