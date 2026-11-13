package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/agent"
	"github.com/LeanMendez/pr-review/internal/app"
	"github.com/LeanMendez/pr-review/internal/app/apptest"
	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/tui/listdata"
	"github.com/LeanMendez/pr-review/internal/tui/screens"
	"github.com/LeanMendez/pr-review/internal/tui/sys"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

// driver plays the Bubble Tea runtime: commands run in goroutines and their
// messages come back through msgs, to be fed to the app in order.
type driver struct {
	t    *testing.T
	app  App
	msgs chan tea.Msg
}

func newDriver(t *testing.T, a App) *driver {
	return &driver{t: t, app: a, msgs: make(chan tea.Msg, 1024)}
}

// send feeds msg to the app and runs the command it returns.
func (d *driver) send(msg tea.Msg) tea.Cmd {
	d.t.Helper()
	m, cmd := d.app.Update(msg)
	d.app = m.(App)
	d.run(cmd)
	return cmd
}

func (d *driver) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		switch msg := cmd().(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range msg {
				d.run(c)
			}
		default:
			d.msgs <- msg
		}
	}()
}

// until feeds the messages that arrive to the app until one matches stop
// (which is fed too) and returns it.
func (d *driver) until(what string, stop func(tea.Msg) bool) tea.Msg {
	d.t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case msg := <-d.msgs:
			d.send(msg)
			if stop(msg) {
				return msg
			}
		case <-timeout:
			d.t.Fatalf("timed out waiting for %s; screen:\n%s", what, d.view())
			return nil
		}
	}
}

func is[T any](msg tea.Msg) bool { _, ok := msg.(T); return ok }

func (d *driver) view() string { return tuitest.Plain(d.app.View().Content) }

func (d *driver) key(k string) tea.Cmd { d.t.Helper(); return d.send(tuitest.Key(k)) }

// notes records the desktop notifications.
type notes struct {
	mu    sync.Mutex
	calls [][]string
}

func (n *notes) all() [][]string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([][]string(nil), n.calls...)
}

func fakeSys(n *notes) sys.System {
	return sys.System{
		Exec: func(_ context.Context, c execx.Cmd) error {
			if c.Name == "notify-send" {
				n.mu.Lock()
				n.calls = append(n.calls, c.Args)
				n.mu.Unlock()
			}
			return nil
		},
		LookPath: func(string) (string, error) { return "/usr/bin/tool", nil },
	}
}

// harness is apptest's with a config on disk, in a private XDG dir.
func harness(t *testing.T) *apptest.Harness {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("BITBUCKET_WORKSPACE", "")
	h := apptest.New(t)
	h.Deps.ConfigFound = true
	return h
}

func row477() listdata.Row {
	return listdata.Row{
		Ctx:       prb.PRContext{PullRequest: prb.PR{ID: 477, Title: "CU-1 Pagos", Author: "Lucía", UpdatedOn: time.Now()}},
		Lines:     1,
		Suggested: pipeline.ModeLigero,
	}
}

func row88() listdata.Row {
	r := row477()
	r.Ctx.PullRequest.ID, r.Ctx.PullRequest.Title = 88, "Refresh token"
	r.Ctx.PullRequest.UpdatedOn = time.Now().Add(-time.Hour)
	return r
}

// listed opens the app on ws/stock with PRs 477 and 88 loaded.
func listed(t *testing.T, h *apptest.Harness, n *notes) *driver {
	t.Helper()
	d := newDriver(t, NewApp(h.Deps, fakeSys(n), "ws/stock"))
	d.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	d.send(screens.RowsLoadedMsg{Ready: []listdata.Row{row477(), row88()}})
	if v := d.view(); !strings.Contains(v, "CU-1 Pagos") {
		t.Fatalf("list not shown:\n%s", v)
	}
	return d
}

func TestAppStartScreens(t *testing.T) {
	h := harness(t)
	cases := []struct {
		name  string
		app   func() App
		found bool
		want  string
	}{
		{"sin configuración", func() App { return NewApp(h.Deps, fakeSys(&notes{}), "ws/stock") }, false, "configuración inicial, paso 1 de 3"},
		{"sin repo", func() App { return NewApp(h.Deps, fakeSys(&notes{}), "") }, true, "Cargando repos de ws…"},
		{"con repo", func() App { return NewApp(h.Deps, fakeSys(&notes{}), "ws/stock") }, true, "Cargando PRs de ws/stock…"},
		{"config models", func() App { return NewModelsApp(h.Deps, fakeSys(&notes{})) }, true, "configuración, modelos"},
		{"config models sin configuración", func() App { return NewModelsApp(h.Deps, fakeSys(&notes{})) }, false, "configuración inicial, paso 1 de 3"},
	}
	for _, c := range cases {
		h.Deps.ConfigFound = c.found
		a := c.app()
		if v := tuitest.Plain(a.View().Content); !strings.Contains(v, c.want) {
			t.Errorf("%s: want %q in:\n%s", c.name, c.want, v)
		}
		if !a.View().AltScreen {
			t.Errorf("%s: not in the alternate screen", c.name)
		}
	}
}

func TestAppFlow(t *testing.T) {
	h := harness(t)
	n := &notes{}
	d := listed(t, h, n)

	d.send(screens.OpenModeMsg{Row: row477()})
	if v := d.view(); !strings.Contains(v, "Elegí cómo revisarlo") {
		t.Fatalf("mode picker not shown:\n%s", v)
	}
	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	if v := d.view(); !strings.Contains(v, "modo ligero") {
		t.Fatalf("progress not shown:\n%s", v)
	}
	if got := d.app.reviews.Running(); got != 1 {
		t.Fatalf("Running() = %d, want 1", got)
	}

	d.until("the report", is[screens.OpenResultMsg])
	v := d.view()
	for _, want := range []string{"Aprobar.", "pr-477-20261112-1030"} {
		if !strings.Contains(v, want) {
			t.Errorf("report: missing %q in:\n%s", want, v)
		}
	}
	if got := d.app.reviews.Running(); got != 0 {
		t.Errorf("Running() = %d after the review, want 0", got)
	}
	if got := n.all(); len(got) != 0 {
		t.Errorf("a review watched to the end notified: %q", got)
	}

	d.send(screens.BackMsg{})
	if v := d.view(); !strings.Contains(v, "lo revisó hace") {
		t.Errorf("back on the list, 477 should show as reviewed:\n%s", v)
	}
}

func TestAppBackground(t *testing.T) {
	h := harness(t)
	h.Agents.Critical = true
	release := make(chan struct{})
	h.Agents.OnRun = func(ctx context.Context, _ agent.Spec) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	n := &notes{}
	d := listed(t, h, n)
	// Unbuffered: the review stalls unless the router drains it off screen.
	d.app.reviews.buffer = 0

	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.key("s")
	d.until("the list", is[screens.BackgroundMsg])
	if v := d.view(); !strings.Contains(v, "CU-1 Pagos") || strings.Contains(v, "modo ligero") {
		t.Fatalf("s should go back to the list:\n%s", v)
	}
	if got := d.app.reviews.Running(); got != 1 {
		t.Fatalf("Running() = %d in the background, want 1", got)
	}

	// enter on a PR under review shows its progress instead of a second review.
	d.key("enter")
	d.until("the progress again", is[screens.OpenModeMsg])
	if v := d.view(); !strings.Contains(v, "modo ligero") {
		t.Fatalf("enter on a running review should show its progress:\n%s", v)
	}
	d.key("s")
	d.until("the list", is[screens.BackgroundMsg])

	close(release)
	d.until("the notification", is[notifiedMsg])
	calls := n.all()
	if len(calls) != 1 || len(calls[0]) != 3 || calls[0][1] != "pr-review" || !strings.HasPrefix(calls[0][2], "#477 listo: Pedir cambios.") {
		t.Fatalf("notifications = %q, want one titled pr-review with #477 listo: Pedir cambios.", calls)
	}
	v := d.view()
	if !strings.Contains(v, "lo revisó hace") || !strings.Contains(v, "o abrir el último reporte") {
		t.Fatalf("477 should show as reviewed and offer its report:\n%s", v)
	}
	if strings.Contains(v, "Rompe pagos") {
		t.Fatalf("a background review must not open its report by itself:\n%s", v)
	}

	d.key("o")
	d.until("the report", is[screens.OpenReportMsg])
	v = d.view()
	for _, want := range []string{"Pedir cambios.", "Rompe pagos.", "pr-477-20261112-1030"} {
		if !strings.Contains(v, want) {
			t.Errorf("report: missing %q in:\n%s", want, v)
		}
	}
}

func TestAppQuitConfirm(t *testing.T) {
	h := harness(t)
	h.Agents.OnRun = func(ctx context.Context, _ agent.Spec) error {
		<-ctx.Done()
		return ctx.Err()
	}
	n := &notes{}
	d := listed(t, h, n)

	// Straight to Update: the driver would queue the QuitMsg for later.
	if _, cmd := d.app.Update(screens.QuitMsg{}); cmd == nil || !is[tea.QuitMsg](cmd()) {
		t.Fatal("with no review running, QuitMsg should quit right away")
	}

	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.until("an agent", func(msg tea.Msg) bool {
		e, ok := msg.(reviewEventMsg)
		return ok && e.ev.Kind == pipeline.AgentStarted
	})

	if cmd := d.send(screens.QuitMsg{}); cmd != nil {
		t.Fatal("QuitMsg with a review running must ask first")
	}
	if v := d.view(); !strings.Contains(v, "Hay 1 revisión en curso. ¿Salir igual? (s/n)") {
		t.Fatalf("no confirmation:\n%s", v)
	}
	d.key("n")
	if v := d.view(); strings.Contains(v, "¿Salir igual?") || !strings.Contains(v, "modo ligero") {
		t.Fatalf("n should go back to the progress:\n%s", v)
	}
	if got := d.app.reviews.Running(); got != 1 {
		t.Fatalf("Running() = %d after n, want 1", got)
	}

	d.send(screens.QuitMsg{})
	if cmd := d.key("s"); cmd != nil {
		t.Fatal("s must wait for the review to end before quitting")
	}
	if v := d.view(); !strings.Contains(v, "Cancelando 1 revisión en curso…") {
		t.Errorf("no cancelling status:\n%s", v)
	}
	done := d.until("the end of the review", is[screens.ReviewDoneMsg]).(screens.ReviewDoneMsg)
	if !errors.Is(done.Err, context.Canceled) {
		t.Errorf("review err = %v, want it cancelled", done.Err)
	}
	if got := h.Removed(); got != 2 {
		t.Errorf("worktrees removed before quitting = %d, want 2", got)
	}
	d.until("quit", is[tea.QuitMsg])
	if got := n.all(); len(got) != 0 {
		t.Errorf("quitting notified: %q", got)
	}
}

func TestAppModelsFromList(t *testing.T) {
	h := harness(t)
	d := listed(t, h, &notes{})

	d.send(screens.OpenModelsMsg{})
	if v := d.view(); !strings.Contains(v, "configuración, modelos") {
		t.Fatalf("models screen not shown:\n%s", v)
	}
	// Roles: superior, resumen, bugs. Bugs goes from opus to sonnet.
	d.key("down")
	d.key("down")
	d.key("right")
	d.key("g")
	saved := d.until("the save", is[screens.ModelsSavedMsg]).(screens.ModelsSavedMsg)
	if got := saved.Cfg.AssignmentFor(config.RoleBugs).Model; got != config.ModelSonnet {
		t.Fatalf("saved bugs model = %s, want sonnet", got)
	}
	if v := d.view(); !strings.Contains(v, "CU-1 Pagos") {
		t.Fatalf("saving should go back to the list:\n%s", v)
	}
	p, _ := config.Path()
	if cfg, found, err := config.Load(p); err != nil || !found || cfg.AssignmentFor(config.RoleBugs).Model != config.ModelSonnet {
		t.Errorf("config on disk: found=%v err=%v", found, err)
	}

	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.until("the report", is[screens.OpenResultMsg])
	var bugs int
	for _, s := range h.Agents.Specs() {
		if s.Role == "bugs" {
			bugs++
			if s.Assignment.Model != config.ModelSonnet {
				t.Errorf("bugs ran on %s, want the saved sonnet", s.Assignment.Model)
			}
		}
	}
	if bugs == 0 {
		t.Error("no bugs agent ran")
	}
}

func TestAppModelsOnlyQuitsOnBack(t *testing.T) {
	h := harness(t)
	d := newDriver(t, NewModelsApp(h.Deps, fakeSys(&notes{})))
	d.key("esc")
	d.until("quit", is[tea.QuitMsg])
}

// TestAppOnboardingThenList walks the first-run setup through the router:
// the screens it nests send their messages back through the program loop.
func TestAppOnboardingThenList(t *testing.T) {
	h := harness(t)
	h.Deps.ConfigFound = false
	inner := h.Deps.Exec
	ex := func(ctx context.Context, c execx.Cmd) error {
		switch {
		case c.Name == "prb" && len(c.Args) > 1 && c.Args[0] == "auth":
			_, _ = c.Stdout.Write([]byte(`{"authenticated":true,"user":"leanm","default_workspace":"otro"}`))
			return nil
		case c.Name == "prb" && len(c.Args) > 1 && c.Args[1] == "list":
			_, _ = c.Stdout.Write([]byte(`[]`))
			return nil
		case c.Name == "claude":
			_, _ = c.Stdout.Write([]byte("2.1.0 (Claude Code)\n"))
			return nil
		}
		return inner(ctx, c)
	}
	h.Deps.Exec, h.Deps.PRB.Exec = ex, ex
	d := newDriver(t, NewApp(h.Deps, fakeSys(&notes{}), "stock"))
	d.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	d.run(d.app.Init())
	d.until("the tools check", func(tea.Msg) bool { return strings.Contains(d.view(), "Claude Code: 2.1.0") })
	d.key("enter") // tools OK
	d.key("enter") // workspace "otro", prefilled from prb
	d.key("enter") // Balanceado
	d.until("the end of the setup", is[screens.OnboardingDoneMsg])
	if v := d.view(); !strings.Contains(v, "otro/stock") {
		t.Fatalf("after the setup, the list of otro/stock:\n%s", v)
	}
	p, _ := config.Path()
	if cfg, found, err := config.Load(p); err != nil || !found || cfg.Workspace != "otro" {
		t.Errorf("config on disk: found=%v workspace=%q err=%v", found, cfg.Workspace, err)
	}
	d.until("the PRs", is[screens.RowsLoadedMsg])
	if v := d.view(); !strings.Contains(v, "No hay PRs listos para revisar en otro/stock.") {
		t.Errorf("list:\n%s", v)
	}
}

func TestAppOpenReportMissing(t *testing.T) {
	h := harness(t)
	d := listed(t, h, &notes{})
	d.send(screens.OpenReportMsg{Repo: "ws/stock", PR: 477})
	if v := d.view(); !strings.Contains(v, "No pude abrir el reporte: no hay reportes de stock#477") {
		t.Errorf("a missing report should say so on the list:\n%s", v)
	}
}

func TestAppRepoPicker(t *testing.T) {
	h := harness(t)
	d := newDriver(t, NewApp(h.Deps, fakeSys(&notes{}), ""))
	d.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	d.send(screens.ReposLoadedMsg{Repos: []prb.Repo{{Slug: "stock", FullName: "ws/stock"}}})
	d.key("enter")
	d.until("the list", is[screens.RepoChosenMsg])
	if v := d.view(); !strings.Contains(v, "Cargando PRs de ws/stock…") {
		t.Fatalf("choosing a repo should open its list:\n%s", v)
	}
}

func TestReviewCmd(t *testing.T) {
	h := harness(t)
	events := make(chan pipeline.Event, 256)
	msg := ReviewCmd(context.Background(), h.Deps, "ws/stock", row477(), pipeline.ModeLigero, events)()
	for range events { // closed by ReviewCmd
	}
	done, ok := msg.(screens.ReviewDoneMsg)
	if !ok || done.PR != 477 || done.Err != nil || done.Warning != nil || done.Result.Verdict != finding.VerdictAprobar {
		t.Fatalf("msg = %#v", msg)
	}
	if want := filepath.Join(h.Reports, "stock", "pr-477-20261112-1030"); done.Dir != want {
		t.Errorf("Dir = %q, want %q", done.Dir, want)
	}

	// A worktree that cannot be removed is a warning on a finished review.
	inner := h.Deps.Cache.Exec
	h.Deps.Cache.Exec = func(ctx context.Context, c execx.Cmd) error {
		if c.Name == "git" && len(c.Args) > 1 && c.Args[0] == "worktree" && c.Args[1] == "remove" {
			return errors.New("worktree ocupado")
		}
		return inner(ctx, c)
	}
	done = ReviewCmd(context.Background(), h.Deps, "ws/stock", row477(), pipeline.ModeLigero, make(chan pipeline.Event, 256))().(screens.ReviewDoneMsg)
	var ce *app.CleanupError
	if done.Err != nil || !errors.As(done.Warning, &ce) || done.Dir == "" {
		t.Errorf("cleanup failure: %#v", done)
	}
	h.Deps.Cache.Exec = inner

	// A report that cannot be written keeps the verdict, with a warning.
	// A directory where context.json goes makes report.Write fail.
	blocker := filepath.Join(h.Reports, "stock", "pr-477-20261112-1030", "context.json")
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocker, 0o700); err != nil {
		t.Fatal(err)
	}
	done = ReviewCmd(context.Background(), h.Deps, "ws/stock", row477(), pipeline.ModeLigero, make(chan pipeline.Event, 256))().(screens.ReviewDoneMsg)
	if done.Err != nil || done.Dir != "" || done.Result.Verdict != finding.VerdictAprobar ||
		done.Warning == nil || !strings.Contains(done.Warning.Error(), "no se pudo guardar el reporte") {
		t.Errorf("report write failure: %#v", done)
	}

	// A review that fails says so.
	h.Deps.PRB.Exec = func(context.Context, execx.Cmd) error { return errors.New("sin red") }
	done = ReviewCmd(context.Background(), h.Deps, "ws/stock", row477(), pipeline.ModeLigero, make(chan pipeline.Event, 256))().(screens.ReviewDoneMsg)
	if done.PR != 477 || done.Err == nil || !strings.Contains(done.Err.Error(), "sin red") {
		t.Errorf("failure: %#v", done)
	}
}
