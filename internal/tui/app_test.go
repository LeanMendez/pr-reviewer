package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	"github.com/LeanMendez/pr-review/internal/tui/theme"
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
	return listedWith(t, h, fakeSys(n))
}

func listedWith(t *testing.T, h *apptest.Harness, s sys.System) *driver {
	t.Helper()
	d := newDriver(t, NewApp(h.Deps, s, "ws/stock"))
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

	d.key("m")
	d.until("the settings", is[screens.OpenSettingsMsg])
	d.key("enter")
	d.until("the models", is[screens.OpenModelsMsg])
	d.shows("enter on Modelos", onModels)
	// Roles: superior, resumen, bugs. Bugs goes from opus to sonnet.
	d.key("down")
	d.key("down")
	d.key("right")
	d.key("g")
	saved := d.until("the save", is[screens.ModelsSavedMsg]).(screens.ModelsSavedMsg)
	if got := saved.Cfg.AssignmentFor(config.RoleBugs).Model; got != config.ModelSonnet {
		t.Fatalf("saved bugs model = %s, want sonnet", got)
	}
	d.shows("saving the models goes back to the settings", onSettings)
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
		case c.Name == "claude" && len(c.Args) > 0 && c.Args[0] == "auth":
			_, _ = c.Stdout.Write([]byte(`{"loggedIn":true}`))
			return nil
		case c.Name == "claude":
			_, _ = c.Stdout.Write([]byte("2.1.0 (Claude Code)\n"))
			return nil
		}
		return inner(ctx, c)
	}
	h.Deps.Exec, h.Deps.PRB.Exec = ex, ex
	// The workspace typed in the setup wins over BITBUCKET_WORKSPACE.
	t.Setenv("BITBUCKET_WORKSPACE", "del-entorno")
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

// blockUntilCancelled makes every agent wait for its review to be cancelled.
func blockUntilCancelled(ctx context.Context, _ agent.Spec) error {
	<-ctx.Done()
	return ctx.Err()
}

func agentStarted(msg tea.Msg) (int, bool) {
	e, ok := msg.(reviewEventMsg)
	return e.pr, ok && e.ev.Kind == pipeline.AgentStarted
}

func TestAppNotifyFailure(t *testing.T) {
	h := harness(t)
	release := make(chan struct{})
	h.Agents.OnRun = func(ctx context.Context, _ agent.Spec) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s := fakeSys(&notes{})
	s.Exec = func(context.Context, execx.Cmd) error { return errors.New("dbus\ncaído") }
	d := listedWith(t, h, s)
	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.send(screens.BackgroundMsg{PR: 477})
	close(release)
	d.until("the notification", is[notifiedMsg])
	if v := d.view(); !strings.Contains(v, "No pude avisar: dbus caído") {
		t.Errorf("a failed notification should show on the list:\n%s", v)
	}
}

// TestAppModelsSaveKeepsConfigClean: saving the models writes back what the
// user configured, not the cache dir pr-review derived.
func TestAppModelsSaveKeepsConfigClean(t *testing.T) {
	for _, c := range []struct{ name, file, want string }{
		{"derived cache dir", "workspace: ws\n", ""},
		{"the user's cache dir", "workspace: ws\ndirectorio_cache: /x/cache\n", "directorio_cache: /x/cache"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfgHome := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", cfgHome)
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			p := filepath.Join(cfgHome, "pr-review", "config.yaml")
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(c.file), 0o600); err != nil {
				t.Fatal(err)
			}
			deps, err := app.DefaultDeps()
			if err != nil {
				t.Fatal(err)
			}
			deps.Exec = execx.Fake(func(execx.Cmd) (string, string, int) { return "", "", 1 })
			d := newDriver(t, NewModelsApp(deps, fakeSys(&notes{})))
			d.key("g")
			d.until("the save", is[screens.ModelsSavedMsg])
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case c.want == "" && strings.Contains(string(data), "directorio_cache"):
				t.Errorf("the derived cache dir was saved:\n%s", data)
			case c.want != "" && !strings.Contains(string(data), c.want):
				t.Errorf("the user's cache dir was lost:\n%s", data)
			}
		})
	}
}

func TestAppForceQuit(t *testing.T) {
	h := harness(t)
	h.Agents.OnRun = blockUntilCancelled
	d := listed(t, h, &notes{})
	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.until("an agent", func(msg tea.Msg) bool { _, ok := agentStarted(msg); return ok })

	d.send(screens.QuitMsg{})
	if v := d.view(); !strings.Contains(v, "ctrl+c sale ya, sin esperar la limpieza") {
		t.Errorf("the question should offer ctrl+c:\n%s", v)
	}
	if m, cmd := d.app.Update(tuitest.Key("ctrl+c")); cmd == nil || !is[tea.QuitMsg](cmd()) || !m.(App).forced {
		t.Error("ctrl+c on the question should quit at once")
	}

	d.key("s")
	if v := d.view(); !strings.Contains(v, "Cancelando 1 revisión en curso…") || !strings.Contains(v, "ctrl+c sale ya, sin esperar la limpieza") {
		t.Errorf("the wait should offer ctrl+c:\n%s", v)
	}
	if m, cmd := d.app.Update(tuitest.Key("ctrl+c")); cmd == nil || !is[tea.QuitMsg](cmd()) || !m.(App).forced {
		t.Error("ctrl+c while waiting should quit at once")
	}
}

// stopperCalls records what shutdown asks of the reviews.
type stopperCalls []string

func (c *stopperCalls) CancelAll() { *c = append(*c, "cancel") }
func (c *stopperCalls) Wait()      { *c = append(*c, "wait") }
func (c *stopperCalls) WaitAtMost(d time.Duration) bool {
	*c = append(*c, "wait at most "+d.String())
	return false
}

// TestShutdown: after the program ends, Run waits for the reviews to clean
// up; when the user forced the quit, only a bounded while, for their
// processes to be killed.
func TestShutdown(t *testing.T) {
	var forced stopperCalls
	shutdown(App{forced: true}, &forced)
	if want := []string{"cancel", "wait at most 2s"}; !slices.Equal(forced, want) {
		t.Errorf("forced quit: %q, want %q", forced, want)
	}
	var normal stopperCalls
	shutdown(App{}, &normal)
	if want := []string{"cancel", "wait"}; !slices.Equal(normal, want) {
		t.Errorf("normal end: %q, want %q", normal, want)
	}

	release := make(chan struct{})
	r := newReviews(context.Background(), func(_ context.Context, _ listdata.Row, _ pipeline.Mode, events chan<- pipeline.Event) tea.Cmd {
		return func() tea.Msg { <-release; close(events); return nil } // a slow cleanup
	})
	r.Start(row477(), pipeline.ModeLigero)
	done := make(chan struct{})
	go func() { shutdown(App{reviews: r}, r); close(done) }()
	select {
	case <-done:
		t.Fatal("a normal end must wait for the reviews")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown never returned")
	}
}

func TestReviewsWaitAtMost(t *testing.T) {
	release := make(chan struct{})
	r := newReviews(context.Background(), func(_ context.Context, _ listdata.Row, _ pipeline.Mode, events chan<- pipeline.Event) tea.Cmd {
		return func() tea.Msg { <-release; close(events); return nil }
	})
	r.Start(row477(), pipeline.ModeLigero)
	start := time.Now()
	if r.WaitAtMost(50 * time.Millisecond) {
		t.Error("WaitAtMost reported the reviews ended while one still runs")
	}
	if d := time.Since(start); d < 50*time.Millisecond || d > 5*time.Second {
		t.Errorf("WaitAtMost(50ms) took %v", d)
	}
	close(release)
	if !r.WaitAtMost(5 * time.Second) {
		t.Error("WaitAtMost gave up although the review ended")
	}
}

// rowLine is the list row of title (the first line that names it).
func rowLine(view, title string) string {
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, title) {
			return l
		}
	}
	return ""
}

func TestAppBackgroundShowsRunning(t *testing.T) {
	h := harness(t)
	release := make(chan struct{})
	h.Agents.OnRun = func(ctx context.Context, _ agent.Spec) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	d := listed(t, h, &notes{})
	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.send(screens.BackgroundMsg{PR: 477})
	v := d.view()
	if l := rowLine(v, "CU-1 Pagos"); !strings.Contains(l, "revisando") {
		t.Errorf("the row under review lacks its mark: %q", l)
	}
	if !strings.Contains(v, "1 revisión en curso") {
		t.Errorf("the header lacks the running count:\n%s", v)
	}
	close(release)
	d.until("the notification", is[notifiedMsg])
	if v := d.view(); strings.Contains(v, "revisando") || strings.Contains(v, "en curso") {
		t.Errorf("a finished review is still marked as running:\n%s", v)
	}
}

func TestAppBackgroundFailureNotesTheList(t *testing.T) {
	h := harness(t)
	h.Deps.PRB.Exec = func(context.Context, execx.Cmd) error { return errors.New("sin red") }
	n := &notes{}
	d := listed(t, h, n)
	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.send(screens.BackgroundMsg{PR: 477})
	d.until("the notification", is[notifiedMsg])
	v := d.view()
	if !strings.Contains(v, "#477: la revisión falló: sin red") {
		t.Errorf("a failed background review must say so on the list:\n%s", v)
	}
	if strings.Contains(rowLine(v, "CU-1 Pagos"), "revisando") || strings.Contains(v, "lo revisó hace") {
		t.Errorf("a failed review is neither running nor reviewed:\n%s", v)
	}
	if calls := n.all(); len(calls) != 1 || calls[0][2] != "#477: la revisión falló: sin red" {
		t.Errorf("notifications = %q", calls)
	}
}

func TestAppBackgroundUnsavedOpensTheResult(t *testing.T) {
	h := harness(t)
	// A directory where context.json goes makes the report fail to save.
	blocker := filepath.Join(h.Reports, "stock", "pr-477-20261112-1030", "context.json")
	if err := os.MkdirAll(blocker, 0o700); err != nil {
		t.Fatal(err)
	}
	d := listed(t, h, &notes{})
	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.send(screens.BackgroundMsg{PR: 477})
	d.until("the notification", is[notifiedMsg])
	v := d.view()
	if !strings.Contains(v, "#477 listo: Aprobar.") || !strings.Contains(v, "no se pudo guardar el reporte") {
		t.Errorf("an unsaved review must say so on the list:\n%s", v)
	}
	if !strings.Contains(v, "lo revisó hace") || !strings.Contains(v, "o abrir el último reporte") {
		t.Fatalf("an unsaved review still offers its result:\n%s", v)
	}
	d.key("o")
	d.until("the report", is[screens.OpenReportMsg])
	v = d.view()
	for _, want := range []string{"Aprobar.", "Aviso: no se pudo guardar el reporte"} {
		if !strings.Contains(v, want) {
			t.Errorf("report: missing %q in:\n%s", want, v)
		}
	}
	if strings.Contains(v, "Guardado en") || strings.Contains(v, "No pude abrir el reporte") {
		t.Errorf("the result was never saved:\n%s", v)
	}

	// A later review of the PR that fails does not lose that result.
	d.send(screens.BackMsg{})
	d.app.s.d.PRB.Exec = func(context.Context, execx.Cmd) error { return errors.New("sin red") }
	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.send(screens.BackgroundMsg{PR: 477})
	d.until("the second notification", is[notifiedMsg])
	d.key("o")
	d.until("the report again", is[screens.OpenReportMsg])
	if v := d.view(); !strings.Contains(v, "Aprobar.") || strings.Contains(v, "No pude abrir el reporte") {
		t.Errorf("the last result was lost to a failed review:\n%s", v)
	}
}

// TestAppCancelThenBackground: c and then s; the engine ends with its own
// error, not context.Canceled, and the review still reads as cancelled.
func TestAppCancelThenBackground(t *testing.T) {
	h := harness(t)
	release := make(chan struct{})
	inner := h.Deps.PRB.Exec
	h.Deps.PRB.Exec = func(ctx context.Context, c execx.Cmd) error {
		if len(c.Args) > 1 && c.Args[1] == "context" {
			<-ctx.Done()
			<-release
			return &execx.ExitError{Code: 137, Stderr: "killed"}
		}
		return inner(ctx, c)
	}
	n := &notes{}
	d := listed(t, h, n)
	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.key("c")
	d.key("s")
	d.until("the list", is[screens.BackgroundMsg])
	close(release)
	done := d.until("the end of the review", is[screens.ReviewDoneMsg]).(screens.ReviewDoneMsg)
	if errors.Is(done.Err, context.Canceled) {
		t.Fatalf("the fake must end with its own error, got %v", done.Err)
	}
	d.until("the notification", is[notifiedMsg])
	const want = "#477: la revisión se canceló."
	if calls := n.all(); len(calls) != 1 || calls[0][2] != want {
		t.Errorf("notifications = %q, want %q", calls, want)
	}
	if v := d.view(); !strings.Contains(v, want) || strings.Contains(v, "falló") {
		t.Errorf("the list should say the review was cancelled:\n%s", v)
	}
}

func TestAppStaleReloadKeepsReviewed(t *testing.T) {
	h := harness(t)
	d := listed(t, h, &notes{})
	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.until("the report", is[screens.OpenResultMsg])
	d.send(screens.BackMsg{})
	// A reload started before the review ended still says "never reviewed".
	d.send(screens.RowsLoadedMsg{Ready: []listdata.Row{row477(), row88()}})
	if v := d.view(); !strings.Contains(v, "lo revisó hace") || !strings.Contains(v, "o abrir el último reporte") {
		t.Errorf("477 lost its review after a stale reload:\n%s", v)
	}
}

func TestAppQuitTwoReviews(t *testing.T) {
	h := harness(t)
	h.Agents.OnRun = blockUntilCancelled
	n := &notes{}
	d := listed(t, h, n)
	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.send(screens.BackgroundMsg{PR: 477})
	d.send(screens.StartReviewMsg{Row: row88(), Mode: pipeline.ModeLigero})
	started := map[int]bool{}
	d.until("an agent of each review", func(msg tea.Msg) bool {
		if pr, ok := agentStarted(msg); ok {
			started[pr] = true
		}
		return len(started) == 2
	})
	if got := d.app.reviews.Running(); got != 2 {
		t.Fatalf("Running() = %d, want 2", got)
	}

	d.send(screens.QuitMsg{})
	if v := d.view(); !strings.Contains(v, "Hay 2 revisiones en curso. ¿Salir igual? (s/n)") {
		t.Fatalf("no confirmation:\n%s", v)
	}
	if cmd := d.key("s"); cmd != nil {
		t.Fatal("s must wait for both reviews")
	}
	if v := d.view(); !strings.Contains(v, "Cancelando 2 revisiones en curso…") {
		t.Errorf("no cancelling status:\n%s", v)
	}
	ended := map[int]bool{}
	d.until("both reviews to end", func(msg tea.Msg) bool {
		if is[tea.QuitMsg](msg) {
			t.Fatalf("quit with %d of 2 reviews ended", len(ended))
		}
		if done, ok := msg.(screens.ReviewDoneMsg); ok {
			if !errors.Is(done.Err, context.Canceled) {
				t.Errorf("#%d ended with %v, want it cancelled", done.PR, done.Err)
			}
			ended[done.PR] = true
		}
		return len(ended) == 2
	})
	if !ended[477] || !ended[88] {
		t.Errorf("ended = %v, want 477 and 88", ended)
	}
	if got := h.Removed(); got != 4 {
		t.Errorf("worktrees removed before quitting = %d, want 4", got)
	}
	d.until("quit", is[tea.QuitMsg])
	if got := n.all(); len(got) != 0 {
		t.Errorf("quitting notified: %q", got)
	}
}

// useDefaultThemeAfter restores the default theme once the test ends.
func useDefaultThemeAfter(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		def, _ := theme.ByName(theme.DefaultName)
		theme.Use(def)
	})
}

func TestAppAppliesConfiguredTheme(t *testing.T) {
	useDefaultThemeAfter(t)
	h := harness(t)
	h.Deps.Cfg.Tema = "claude-code"
	NewApp(h.Deps, fakeSys(&notes{}), "ws/stock")
	if got := theme.Current().Name; got != "claude-code" {
		t.Fatalf("theme = %q, want claude-code", got)
	}
}

func TestThemeFor(t *testing.T) {
	cfg := config.Defaults()
	if th, note := themeFor(cfg); th.Name != theme.DefaultName || note != "" {
		t.Errorf("empty: %q, %q", th.Name, note)
	}
	cfg.Tema = "noir"
	if th, note := themeFor(cfg); th.Name != "noir" || note != "" {
		t.Errorf("known: %q, %q", th.Name, note)
	}
	cfg.Tema = "vaporwave"
	if th, note := themeFor(cfg); th.Name != theme.DefaultName || note != "No conozco el tema vaporwave; uso pr-review." {
		t.Errorf("unknown: %q, %q", th.Name, note)
	}
}

func TestAppUnknownThemeNote(t *testing.T) {
	useDefaultThemeAfter(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	p := filepath.Join(cfgHome, "pr-review", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("workspace: ws\ntema: vaporwave\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, err := app.DefaultDeps()
	if err != nil {
		t.Fatal(err)
	}
	deps.Exec = execx.Fake(func(execx.Cmd) (string, string, int) { return "", "", 1 })
	d := newDriver(t, NewApp(deps, fakeSys(&notes{}), "ws/stock"))
	d.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	if got := theme.Current().Name; got != theme.DefaultName {
		t.Errorf("theme = %q, want the default", got)
	}
	const note = "No conozco el tema vaporwave; uso pr-review."
	d.send(screens.RowsLoadedMsg{Ready: []listdata.Row{row477()}})
	if v := d.view(); !strings.Contains(v, note) {
		t.Fatalf("the load erased the note:\n%s", v)
	}

	// Saving the models keeps the text of the theme.
	d.key("m")
	d.until("the settings", is[screens.OpenSettingsMsg])
	d.key("enter")
	d.until("the models", is[screens.OpenModelsMsg])
	d.key("g")
	d.until("the save", is[screens.ModelsSavedMsg])
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "tema: vaporwave") {
		t.Errorf("tema was not kept:\n%s", data)
	}
}

func TestAppViewFillsWholeScreen(t *testing.T) {
	const w, h = 100, 30
	for _, name := range []string{"claude-code", "sensei"} {
		th, ok := theme.ByName(name)
		if !ok {
			t.Fatal(name)
		}
		check := func(t *testing.T, view, where string) {
			t.Helper()
			assertFilled(t, view, name+"/"+where, th.BG, w, h)
		}
		t.Run(name, func(t *testing.T) {
			useDefaultThemeAfter(t)
			hh := harness(t)
			hh.Deps.Cfg.Tema = name
			d := newDriver(t, NewApp(hh.Deps, fakeSys(&notes{}), "ws/stock"))
			d.send(tea.WindowSizeMsg{Width: w, Height: h})
			d.send(screens.RowsLoadedMsg{Ready: []listdata.Row{row477(), row88()}})
			check(t, d.app.View().Content, "list")

			d.app.confirming = true
			check(t, d.app.View().Content, "quit")

			hh2 := harness(t)
			hh2.Deps.Cfg.Tema = name
			hh2.Deps.ConfigFound = false
			inner := hh2.Deps.Exec
			ex := func(ctx context.Context, c execx.Cmd) error {
				switch {
				case c.Name == "prb":
					_, _ = c.Stdout.Write([]byte(`{"authenticated":true,"user":"leanm","default_workspace":"otro"}`))
					return nil
				case c.Name == "claude" && len(c.Args) > 0 && c.Args[0] == "auth":
					_, _ = c.Stdout.Write([]byte(`{"loggedIn":true}`))
					return nil
				case c.Name == "claude":
					_, _ = c.Stdout.Write([]byte("2.1.0 (Claude Code)\n"))
					return nil
				}
				return inner(ctx, c)
			}
			hh2.Deps.Exec, hh2.Deps.PRB.Exec = ex, ex
			o := newDriver(t, NewApp(hh2.Deps, fakeSys(&notes{}), "stock"))
			o.send(tea.WindowSizeMsg{Width: w, Height: h})
			o.run(o.app.Init())
			o.until("the tools check", func(tea.Msg) bool { return strings.Contains(o.view(), "Claude Code: 2.1.0") })
			o.key("enter")
			if v := o.view(); !strings.Contains(v, "workspace") {
				t.Fatalf("workspace step not shown:\n%s", v)
			}
			check(t, o.app.View().Content, "onboarding")
		})
	}
}

// assertFilled checks that view covers w×h cells, all on the background bg
// and none with the terminal's default foreground.
func assertFilled(t *testing.T, view, where, bg string, w, h int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Errorf("%s: %d lines, want %d", where, len(lines), h)
	}
	want := strings.ToUpper(bg)
	for y, l := range lines {
		width := 0
		for x, c := range tuitest.Cells(l) {
			width += c.Width
			if c.BG != want {
				t.Fatalf("%s: line %d cell %d %q has background %q, want %s", where, y, x, c.Text, c.BG, want)
			}
			if c.FG == "" && strings.TrimSpace(c.Text) != "" {
				t.Fatalf("%s: line %d cell %d %q has the default foreground", where, y, x, c.Text)
			}
		}
		if width != w {
			t.Fatalf("%s: line %d is %d cells, want %d", where, y, width, w)
		}
	}
}

// Texts that tell the screens of the settings apart.
const (
	onSettings = "los colores de la interfaz"
	onModels   = "configuración, modelos"
	onThemes   = "configuración, tema"
	onList     = "CU-1 Pagos"
)

func (d *driver) shows(what, want string) {
	d.t.Helper()
	if v := d.view(); !strings.Contains(v, want) {
		d.t.Fatalf("%s: want %q in:\n%s", what, want, v)
	}
}

// openThemes goes from the list to the theme picker: m, Tema.
func (d *driver) openThemes() {
	d.t.Helper()
	d.key("m")
	d.until("the settings", is[screens.OpenSettingsMsg])
	d.key("down")
	d.key("enter")
	d.until("the themes", is[screens.OpenThemesMsg])
	d.shows("enter on Tema", onThemes)
}

func TestAppSettingsNavigation(t *testing.T) {
	h := harness(t)
	d := listed(t, h, &notes{})

	d.key("m")
	d.until("the settings", is[screens.OpenSettingsMsg])
	d.shows("m on the list", onSettings)
	d.key("enter")
	d.until("the models", is[screens.OpenModelsMsg])
	d.shows("enter on Modelos", onModels)
	d.key("esc")
	d.until("back", is[screens.BackMsg])
	d.shows("esc on the models", onSettings)

	d.key("down")
	d.key("enter")
	d.until("the themes", is[screens.OpenThemesMsg])
	d.shows("enter on Tema", onThemes)
	d.key("esc")
	d.until("back", is[screens.BackMsg])
	d.shows("esc on the themes", onSettings)

	d.key("esc")
	d.until("back", is[screens.BackMsg])
	d.shows("esc on the settings", onList)

	// pr-review config models still opens the models alone; esc leaves.
	m := newDriver(t, NewModelsApp(h.Deps, fakeSys(&notes{})))
	m.shows("config models", onModels)
	m.key("esc")
	m.until("quit", is[tea.QuitMsg])
}

func TestAppThemeChosen(t *testing.T) {
	useDefaultThemeAfter(t)
	h := harness(t)
	h.Deps.Cfg.Concurrency = 7
	h.Deps.Cfg.Preset = config.PresetPotente
	h.Deps.Cfg.Models = config.PresetAssignments(config.PresetPotente)
	d := listed(t, h, &notes{})
	p, _ := config.Path()

	d.openThemes()
	d.send(screens.ThemeChosenMsg{Name: "noir"})
	want := h.Deps.Cfg
	want.Tema = "noir"
	if got, found, err := config.Load(p); err != nil || !found || !reflect.DeepEqual(got, want) {
		t.Errorf("config on disk: found=%v err=%v\n got %+v\nwant %+v", found, err, got, want)
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), "tema: noir") {
		t.Errorf("config.yaml lacks tema: noir:\n%s", data)
	}
	if got := theme.Current().Name; got != "noir" {
		t.Errorf("theme = %q, want noir", got)
	}
	d.shows("a saved theme", onSettings)

	// A failed save says so on the picker and changes nothing.
	d.app.save = func(config.Config) error { return errors.New("disco\nlleno") }
	d.key("enter") // Tema is still selected
	d.until("the themes", is[screens.OpenThemesMsg])
	d.send(screens.ThemeChosenMsg{Name: "sensei"})
	d.shows("a failed save", onThemes)
	d.shows("a failed save", "No se pudo guardar: disco lleno")
	if got := theme.Current().Name; got != "noir" {
		t.Errorf("theme = %q after a failed save, want noir", got)
	}
	if got := d.app.s.d.Cfg.Tema; got != "noir" {
		t.Errorf("config in memory has tema %q after a failed save, want noir", got)
	}
}

// TestAppThemeSwitchWithBackgroundReview: a theme chosen while a review runs
// in the background repaints its progress too.
func TestAppThemeSwitchWithBackgroundReview(t *testing.T) {
	useDefaultThemeAfter(t)
	h := harness(t)
	release := make(chan struct{})
	h.Agents.OnRun = func(ctx context.Context, _ agent.Spec) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	d := listed(t, h, &notes{})
	// Unbuffered: the review stalls unless the router drains it off screen.
	d.app.reviews.buffer = 0
	d.send(screens.StartReviewMsg{Row: row477(), Mode: pipeline.ModeLigero})
	d.key("s")
	d.until("the list", is[screens.BackgroundMsg])

	d.openThemes()
	for range len(theme.All()) - 1 { // sensei is the last
		d.key("down")
	}
	d.key("enter")
	d.until("the theme", is[screens.ThemeChosenMsg])
	if got := theme.Current().Name; got != "sensei" {
		t.Fatalf("theme = %q, want sensei", got)
	}
	d.key("esc")
	d.until("the list", is[screens.BackMsg])
	d.key("enter")
	d.until("the progress", is[screens.OpenModeMsg])
	d.shows("enter on the running review", "modo ligero")
	assertFilled(t, d.app.View().Content, "progress", "#F5F2EB", 120, 40)

	close(release)
	d.until("the report", is[screens.OpenResultMsg])
}

// TestAppThemePickerIgnoresKeysUntilAnswered: enter then esc in the same batch
// of input must not let the esc leave the settings after the save.
func TestAppThemePickerIgnoresKeysUntilAnswered(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("save fails=%v", fail), func(t *testing.T) {
			useDefaultThemeAfter(t)
			h := harness(t)
			d := listed(t, h, &notes{})
			d.openThemes()
			if fail {
				d.app.save = func(config.Config) error { return errors.New("disco lleno") }
			}
			// Both keys are handled before any message comes back; their
			// messages are then delivered in order, as the runtime may.
			var queued []tea.Msg
			for _, k := range []string{"enter", "esc"} {
				m, cmd := d.app.Update(tuitest.Key(k))
				d.app = m.(App)
				if cmd != nil {
					queued = append(queued, cmd())
				}
			}
			for _, msg := range queued {
				if msg != nil {
					d.send(msg)
				}
			}
			if fail {
				d.shows("a failed save", onThemes)
				d.shows("a failed save", "No se pudo guardar: disco lleno")
				// The picker answers keys again.
				d.key("esc")
				d.until("back", is[screens.BackMsg])
				d.shows("esc after the error", onSettings)
				return
			}
			d.shows("a saved theme", onSettings)
		})
	}
}
