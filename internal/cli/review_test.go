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
	if _, err := os.Stat(filepath.Join(h.reports, "stock")); err == nil {
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
