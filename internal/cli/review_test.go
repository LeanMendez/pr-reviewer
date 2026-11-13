package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LeanMendez/pr-review/internal/agent"
	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/prompts"
	"github.com/LeanMendez/pr-review/internal/repocache"
)

// fakeAgents answers by role with an empty, clean review. onRun, when set,
// runs first and may fail the agent.
type fakeAgents struct {
	mu    sync.Mutex
	specs []agent.Spec
	onRun func(agent.Spec) error
	// critical makes bugs report a critico finding that the validator confirms.
	critical bool
}

func (f *fakeAgents) Run(_ context.Context, s agent.Spec, _ func(agent.Activity)) (json.RawMessage, error) {
	f.mu.Lock()
	f.specs = append(f.specs, s)
	f.mu.Unlock()
	if f.onRun != nil {
		if err := f.onRun(s); err != nil {
			return nil, err
		}
	}
	var r any
	switch s.Role {
	case prompts.RoleResumen:
		r = map[string]any{"resumen": "Agrega pagos.", "triviales": []string{}}
	case prompts.RoleSuperiorPlan:
		r = map[string]any{"especialistas": []any{
			map[string]any{"rol": "bugs", "grupos": [][]string{{"src/a.go"}}},
			map[string]any{"rol": "reglas", "grupos": [][]string{{"src/a.go"}}},
		}}
	case prompts.RoleSuperiorSintesis:
		r = map[string]any{"frase": "Nada importante.", "division": map[string]any{"necesaria": false, "partes": []any{}}}
	case prompts.RoleJuez:
		r = map[string]any{"votos": []any{}}
	case prompts.RoleValidadorBugs, prompts.RoleValidadorReglas:
		r = map[string]any{"resultado": "confirmado", "origen": "introducido", "evidencia": []string{"src/a.go:2"}, "motivo": "ok"}
	case prompts.RoleBugs:
		hs := []any{}
		if f.critical {
			hs = append(hs, map[string]any{
				"archivo": "src/a.go", "linea": 2, "severidad": "critico", "origen": "introducido", "grupo": "A",
				"que_pasa": "falla", "por_que": "Rompe pagos.", "solucion": "- a\n+ b", "evidencia": []string{"src/a.go:2"},
			})
		}
		r = map[string]any{"hallazgos": hs, "dudas": []any{}}
	default:
		r = map[string]any{"hallazgos": []any{}, "dudas": []any{}}
	}
	return json.Marshal(r)
}

const ctxJSON = `{"repository":"ws/stock","pull_request":{"id":477,"title":"CU-1 Pagos","state":"OPEN",
"source":{"branch":"f","commit":"abc","repository":"ws/stock"},"destination":{"branch":"main","commit":"def","repository":"ws/stock"}},
"files":{"count":1,"lines_added":1,"lines_removed":0,"files":[{"path":"src/a.go","status":"modified","lines_added":1,"lines_removed":0}]}}`

const diffJSON = `{"total_files":1,"files":[{"path":"src/a.go","status":"modified","lines_added":1,"lines_removed":0,
"patch":"@@ -1,1 +1,2 @@\n x\n+y\n"}]}`

type harness struct {
	deps    Deps
	out     bytes.Buffer
	errw    bytes.Buffer
	reports string
	agents  *fakeAgents
	mu      sync.Mutex
	removed int
	calls   []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{reports: t.TempDir(), agents: &fakeAgents{}}
	cfg := config.Defaults()
	cfg.Workspace = "ws"
	cfg.ReportsDir = h.reports
	ex := execx.Fake(func(c execx.Cmd) (string, string, int) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.calls = append(h.calls, c.Name+" "+strings.Join(c.Args, " "))
		switch c.Name {
		case "prb":
			if len(c.Args) > 1 && c.Args[1] == "context" {
				return ctxJSON, "", 0
			}
			return diffJSON, "", 0
		case "git":
			switch {
			case c.Args[0] == "rev-parse":
				return "0123456789abcdef0123\n", "", 0
			case len(c.Args) > 3 && c.Args[0] == "worktree" && c.Args[1] == "add":
				_ = os.MkdirAll(c.Args[3], 0o755)
				h.removed = 0 // Prepare's own leftovers cleanup does not count
			case len(c.Args) > 1 && c.Args[0] == "worktree" && c.Args[1] == "remove":
				h.removed++
			}
		}
		return "", "", 0
	})
	h.deps = Deps{
		Cfg:    cfg,
		PRB:    prb.Client{Exec: ex},
		Cache:  repocache.Cache{Exec: ex, Root: t.TempDir(), CloneURL: cfg.CloneURL},
		Agents: h.agents,
		Exec:   ex,
		Now:    func() time.Time { return time.Date(2026, 11, 12, 10, 30, 0, 0, time.UTC) },
		Out:    &h.out,
		Err:    &h.errw,
	}
	return h
}

// run executes the review command and returns its exit code.
func (h *harness) run(ctx context.Context, args ...string) (int, error) {
	root := newRootCmd("test", func() (Deps, error) { return h.deps, nil })
	root.SetOut(&h.out)
	root.SetErr(&h.errw)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	return exitCodeOf(err), err
}

func TestReviewEndToEndFake(t *testing.T) {
	h := newHarness(t)
	code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "ligero")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stderr=%s", code, err, h.errw.String())
	}
	if !strings.Contains(h.errw.String(), "✓ 1 Chequeos sin IA") {
		t.Fatalf("falta línea de etapa: %q", h.errw.String())
	}
	mdPath := filepath.Join(h.reports, "stock", "pr-477-20261112-1030", "review.md")
	if _, err := os.Stat(mdPath); err != nil {
		t.Fatalf("falta el reporte: %v", err)
	}
	if !strings.Contains(h.out.String(), "Aprobar") && !strings.Contains(h.out.String(), "aprobar") {
		t.Fatalf("falta veredicto en Out: %q", h.out.String())
	}
	if !strings.Contains(h.out.String(), mdPath) {
		t.Fatalf("falta la ruta en Out: %q", h.out.String())
	}
	if h.removed != 2 {
		t.Fatalf("worktrees removidos = %d, quiero 2", h.removed)
	}
}

func TestReviewBadMode(t *testing.T) {
	h := newHarness(t)
	code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "rapido")
	if code != 2 {
		t.Fatalf("code = %d, quiero 2", code)
	}
	if err == nil || err.Error() != "Modo inválido: usá ligero, exhaustivo o judgment." {
		t.Fatalf("err = %v", err)
	}
	if len(h.calls) != 0 {
		t.Fatalf("no debería llamar a nada: %v", h.calls)
	}
}

func TestReviewBadArgsIsUsageError(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"review", "stock"}, {"review", "stock", "abc"}, {"review", "stock", "1", "--nope"},
		{"review", "stock", "477"}, {"review", "stock", "477", "--mode", ""}, {"revisar", "stock", "477"}} {
		if code, _ := h.run(context.Background(), args...); code != 2 {
			t.Errorf("%v: code = %d, quiero 2", args, code)
		}
	}
}

func TestReviewNonApproveVerdict(t *testing.T) {
	h := newHarness(t)
	h.agents.critical = true
	code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "ligero")
	if code != 20 {
		t.Fatalf("code=%d err=%v stderr=%s", code, err, h.errw.String())
	}
	if handleError(err, &bytes.Buffer{}) != 20 {
		t.Fatal("handleError debe propagar 20")
	}
	mdPath := filepath.Join(h.reports, "stock", "pr-477-20261112-1030", "review.md")
	if _, statErr := os.Stat(mdPath); statErr != nil {
		t.Fatalf("falta review.md: %v", statErr)
	}
	if !strings.Contains(h.out.String(), "Pedir cambios") || !strings.Contains(h.out.String(), mdPath) {
		t.Fatalf("stdout: %q", h.out.String())
	}
}

func TestReviewNilNow(t *testing.T) {
	h := newHarness(t)
	h.deps.Now = nil
	if code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "ligero"); code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestReviewForkError(t *testing.T) {
	h := newHarness(t)
	forkCtx := strings.Replace(ctxJSON, `"repository":"ws/stock"},"destination"`, `"repository":"otro/stock"},"destination"`, 1)
	inner := h.deps.PRB.Exec
	h.deps.PRB.Exec = func(ctx context.Context, c execx.Cmd) error {
		if c.Name == "prb" && len(c.Args) > 1 && c.Args[1] == "context" {
			_, _ = c.Stdout.Write([]byte(forkCtx))
			return nil
		}
		return inner(ctx, c)
	}
	code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "ligero")
	if code != 1 || !errors.Is(err, repocache.ErrForkUnsupported) {
		t.Fatalf("code=%d err=%v", code, err)
	}
	var buf bytes.Buffer
	handleError(err, &buf)
	if !strings.Contains(buf.String(), "fork") {
		t.Fatalf("mensaje: %q", buf.String())
	}
	if _, statErr := os.Stat(filepath.Join(h.reports, "stock")); statErr == nil {
		t.Fatal("no debería escribir el reporte")
	}
}

func TestWorktreesCleanedUp(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.agents.onRun = func(agent.Spec) error { cancel(); return errors.New("boom") }
	code, _ := h.run(ctx, "review", "stock", "477", "--mode", "ligero")
	if code != 1 {
		t.Fatalf("code = %d, quiero 1", code)
	}
	if h.removed != 2 {
		t.Fatalf("worktrees removidos = %d, quiero 2", h.removed)
	}
	if _, err := os.Stat(filepath.Join(h.reports, "stock", "pr-477-20261112-1030")); err == nil {
		t.Fatal("no debería escribir el reporte")
	}
}

func TestReviewSetsLogDir(t *testing.T) {
	h := newHarness(t)
	h.deps.Agents = agent.Runner{Exec: h.deps.Exec, Bin: "claude", Timeout: time.Minute, Sem: make(chan struct{}, 1)}
	_, _ = h.run(context.Background(), "review", "stock", "477", "--mode", "ligero")
	logs, _ := filepath.Glob(filepath.Join(h.reports, "stock", "pr-477-20261112-1030", "agentes", "*.jsonl"))
	if len(logs) == 0 {
		t.Fatalf("no hay logs de agentes; stderr=%s", h.errw.String())
	}
}

func TestExitCode(t *testing.T) {
	for v, want := range map[finding.Verdict]int{
		finding.VerdictAprobar: 0, finding.VerdictAprobarConComentarios: 10,
		finding.VerdictPedirCambios: 20, finding.VerdictRevisarAMano: 30,
	} {
		if got := ExitCode(v); got != want {
			t.Errorf("%s: %d, quiero %d", v, got, want)
		}
	}
}

func TestUnknownVerdictHasMessage(t *testing.T) {
	code, err := verdictResult("raro")
	if code != 1 || err == nil || !strings.Contains(err.Error(), "veredicto desconocido: raro") {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestExecuteErrorPrintedOnce(t *testing.T) {
	var buf bytes.Buffer
	if code := handleError(&exitError{Code: 20}, &buf); code != 20 || buf.Len() != 0 {
		t.Fatalf("verdict: code=%d out=%q", code, buf.String())
	}
	if code := handleError(errors.New("x"), &buf); code != 1 || buf.String() != "error: x\n" {
		t.Fatalf("plain: code=%d out=%q", code, buf.String())
	}
}

func TestResolveRepo(t *testing.T) {
	t.Setenv("BITBUCKET_WORKSPACE", "")
	cases := []struct{ arg, ws, want, err string }{
		{"stock", "ws", "ws/stock", ""},
		{"otro/stock", "ws", "otro/stock", ""},
		{"stock", "", "", "Falta el workspace: configuralo con pr-review o pasá workspace/repo."},
	}
	for _, c := range cases {
		got, err := ResolveRepo(c.arg, c.ws)
		if c.err != "" {
			if err == nil || err.Error() != c.err {
				t.Errorf("%q: err = %v", c.arg, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q: got %q, %v", c.arg, got, err)
		}
	}
	t.Setenv("BITBUCKET_WORKSPACE", "envws")
	if got, err := ResolveRepo("stock", ""); err != nil || got != "envws/stock" {
		t.Errorf("env: %q %v", got, err)
	}
}

func TestReportDirPrivateBeforeAgents(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.reports, "stock", "pr-477-20261112-1030")
	var mu sync.Mutex // agents run in parallel
	var mode os.FileMode
	h.agents.onRun = func(agent.Spec) error {
		mu.Lock()
		defer mu.Unlock()
		if st, err := os.Stat(dir); err == nil {
			mode = st.Mode().Perm()
		}
		return nil
	}
	if code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "ligero"); code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if mode != 0o700 {
		t.Errorf("report dir mode %v when the agents ran, want 0700 before the pipeline starts", mode)
	}
}

func TestAgentLogsPrivate(t *testing.T) {
	h := newHarness(t)
	h.deps.Agents = agent.Runner{Exec: h.deps.Exec, Bin: "claude", Timeout: time.Minute, Sem: make(chan struct{}, 1)}
	_, _ = h.run(context.Background(), "review", "stock", "477", "--mode", "ligero")
	dir := filepath.Join(h.reports, "stock", "pr-477-20261112-1030")
	logs, _ := filepath.Glob(filepath.Join(dir, "agentes", "*.jsonl"))
	if len(logs) == 0 {
		t.Fatal("no agent logs")
	}
	for _, p := range append([]string{dir, filepath.Join(dir, "agentes")}, logs...) {
		want := os.FileMode(0o600)
		if st, _ := os.Stat(p); st.IsDir() {
			want = 0o700
		}
		if st, _ := os.Stat(p); st.Mode().Perm() != want {
			t.Errorf("%s: mode %v, want %v", p, st.Mode().Perm(), want)
		}
	}
}

func TestReviewDirect(t *testing.T) {
	h := newHarness(t)
	events := make(chan pipeline.Event, 256)
	res, dir, err := Review(context.Background(), h.deps, "stock", 477, pipeline.ModeLigero, events)
	close(events)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(h.reports, "stock", "pr-477-20261112-1030"); dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
	if res.Verdict != finding.VerdictAprobar || res.PR.ID != 477 || res.Repo != "ws/stock" {
		t.Errorf("result = %+v", res)
	}
	for _, f := range []string{"review.md", "findings.json", "context.json", "pr.diff"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	var stages int
	for e := range events {
		if e.Kind == pipeline.StageDone {
			stages++
		}
	}
	if stages != 6 {
		t.Errorf("%d StageDone events, want 6", stages)
	}
	if h.out.Len() != 0 || h.errw.Len() != 0 {
		t.Errorf("Review printed: out=%q err=%q", h.out.String(), h.errw.String())
	}
	if h.removed != 2 {
		t.Errorf("worktrees removed = %d, want 2", h.removed)
	}
}

func TestReviewCleanupFailureIsAWarning(t *testing.T) {
	h := newHarness(t)
	inner := h.deps.Cache.Exec
	h.deps.Cache.Exec = func(ctx context.Context, c execx.Cmd) error {
		if c.Name == "git" && len(c.Args) > 1 && c.Args[0] == "worktree" && c.Args[1] == "remove" {
			return errors.New("worktree ocupado")
		}
		return inner(ctx, c)
	}
	res, _, err := Review(context.Background(), h.deps, "stock", 477, pipeline.ModeLigero, nil)
	var ce *CleanupError
	if !errors.As(err, &ce) || res.Verdict != finding.VerdictAprobar {
		t.Fatalf("err = %v (%T), verdict %q: want a *CleanupError with the full result", err, err, res.Verdict)
	}
	h.out.Reset()
	if code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "ligero"); code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !strings.Contains(h.errw.String(), "aviso: no se pudieron borrar los worktrees") || !strings.Contains(h.out.String(), "review.md") {
		t.Errorf("out=%q err=%q", h.out.String(), h.errw.String())
	}
}

func TestVerdictShownWhenReportWriteFails(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.reports, "stock", "pr-477-20261112-1030")
	// A directory where context.json goes makes report.Write fail.
	if err := os.MkdirAll(filepath.Join(dir, "context.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "ligero")
	if code != 1 || err == nil || !strings.Contains(err.Error(), "context.json") {
		t.Fatalf("code=%d err=%v, want 1 and the write error", code, err)
	}
	if !strings.Contains(h.out.String(), "**Aprobar.**") {
		t.Errorf("the verdict must be shown even when the report cannot be written: %q", h.out.String())
	}
}

func TestDefaultDepsReportsConfigFound(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	d, err := DefaultDeps()
	if err != nil || d.ConfigFound {
		t.Fatalf("no file: found=%v err=%v", d.ConfigFound, err)
	}
	p := filepath.Join(cfgHome, "pr-review", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("workspace: ws\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if d, err = DefaultDeps(); err != nil || !d.ConfigFound || d.Cfg.Workspace != "ws" {
		t.Fatalf("with file: found=%v workspace=%q err=%v", d.ConfigFound, d.Cfg.Workspace, err)
	}
}
