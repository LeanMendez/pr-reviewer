package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LeanMendez/pr-review/internal/agent"
	"github.com/LeanMendez/pr-review/internal/app"
	"github.com/LeanMendez/pr-review/internal/app/apptest"
	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/repocache"
	"github.com/LeanMendez/pr-review/internal/tui"
)

// harness runs the review command against apptest's fakes.
type harness struct{ *apptest.Harness }

func newHarness(t *testing.T) *harness { return &harness{apptest.New(t)} }

// noUI fails a command that opens the interface by mistake.
func noUI(context.Context, app.Deps, tui.Start) error { return errors.New("the interface opened") }

// run executes the review command and returns its exit code.
func (h *harness) run(ctx context.Context, args ...string) (int, error) {
	root := newRootCmd("test", func() (app.Deps, error) { return h.Deps, nil }, noUI)
	root.SetOut(&h.Out)
	root.SetErr(&h.Err)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	return exitCodeOf(err), err
}

func TestReviewEndToEndFake(t *testing.T) {
	h := newHarness(t)
	code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "ligero")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stderr=%s", code, err, h.Err.String())
	}
	if !strings.Contains(h.Err.String(), "✓ 1 Chequeos sin IA") {
		t.Fatalf("falta línea de etapa: %q", h.Err.String())
	}
	mdPath := filepath.Join(h.Reports, "stock", "pr-477-20261112-1030", "review.md")
	if _, err := os.Stat(mdPath); err != nil {
		t.Fatalf("falta el reporte: %v", err)
	}
	if !strings.Contains(h.Out.String(), "Aprobar") && !strings.Contains(h.Out.String(), "aprobar") {
		t.Fatalf("falta veredicto en Out: %q", h.Out.String())
	}
	if !strings.Contains(h.Out.String(), mdPath) {
		t.Fatalf("falta la ruta en Out: %q", h.Out.String())
	}
	if h.Removed() != 2 {
		t.Fatalf("worktrees removidos = %d, quiero 2", h.Removed())
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
	if len(h.Calls()) != 0 {
		t.Fatalf("no debería llamar a nada: %v", h.Calls())
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
	h.Agents.Critical = true
	code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "ligero")
	if code != 20 {
		t.Fatalf("code=%d err=%v stderr=%s", code, err, h.Err.String())
	}
	if handleError(err, &bytes.Buffer{}) != 20 {
		t.Fatal("handleError debe propagar 20")
	}
	mdPath := filepath.Join(h.Reports, "stock", "pr-477-20261112-1030", "review.md")
	if _, statErr := os.Stat(mdPath); statErr != nil {
		t.Fatalf("falta review.md: %v", statErr)
	}
	if !strings.Contains(h.Out.String(), "Pedir cambios") || !strings.Contains(h.Out.String(), mdPath) {
		t.Fatalf("stdout: %q", h.Out.String())
	}
}

func TestReviewNilNow(t *testing.T) {
	h := newHarness(t)
	h.Deps.Now = nil
	if code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "ligero"); code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestReviewForkError(t *testing.T) {
	h := newHarness(t)
	forkCtx := strings.Replace(apptest.ContextJSON, `"repository":"ws/stock"},"destination"`, `"repository":"otro/stock"},"destination"`, 1)
	inner := h.Deps.PRB.Exec
	h.Deps.PRB.Exec = func(ctx context.Context, c execx.Cmd) error {
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
	if _, statErr := os.Stat(filepath.Join(h.Reports, "stock")); statErr == nil {
		t.Fatal("no debería escribir el reporte")
	}
}

func TestWorktreesCleanedUp(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.Agents.OnRun = func(context.Context, agent.Spec) error { cancel(); return errors.New("boom") }
	code, _ := h.run(ctx, "review", "stock", "477", "--mode", "ligero")
	if code != 1 {
		t.Fatalf("code = %d, quiero 1", code)
	}
	if h.Removed() != 2 {
		t.Fatalf("worktrees removidos = %d, quiero 2", h.Removed())
	}
	if _, err := os.Stat(filepath.Join(h.Reports, "stock", "pr-477-20261112-1030")); err == nil {
		t.Fatal("no debería escribir el reporte")
	}
}

func TestReviewSetsLogDir(t *testing.T) {
	h := newHarness(t)
	h.Deps.Agents = agent.Runner{Exec: h.Deps.Exec, Bin: "claude", Timeout: time.Minute, Sem: make(chan struct{}, 1)}
	_, _ = h.run(context.Background(), "review", "stock", "477", "--mode", "ligero")
	logs, _ := filepath.Glob(filepath.Join(h.Reports, "stock", "pr-477-20261112-1030", "agentes", "*.jsonl"))
	if len(logs) == 0 {
		t.Fatalf("no hay logs de agentes; stderr=%s", h.Err.String())
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

func TestReportDirPrivateBeforeAgents(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.Reports, "stock", "pr-477-20261112-1030")
	var mu sync.Mutex // agents run in parallel
	var mode os.FileMode
	h.Agents.OnRun = func(context.Context, agent.Spec) error {
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
	h.Deps.Agents = agent.Runner{Exec: h.Deps.Exec, Bin: "claude", Timeout: time.Minute, Sem: make(chan struct{}, 1)}
	_, _ = h.run(context.Background(), "review", "stock", "477", "--mode", "ligero")
	dir := filepath.Join(h.Reports, "stock", "pr-477-20261112-1030")
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

func TestReviewCleanupFailureIsAWarning(t *testing.T) {
	h := newHarness(t)
	inner := h.Deps.Cache.Exec
	h.Deps.Cache.Exec = func(ctx context.Context, c execx.Cmd) error {
		if c.Name == "git" && len(c.Args) > 1 && c.Args[0] == "worktree" && c.Args[1] == "remove" {
			return errors.New("worktree ocupado")
		}
		return inner(ctx, c)
	}
	if code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "ligero"); code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !strings.Contains(h.Err.String(), "aviso: no se pudieron borrar los worktrees") || !strings.Contains(h.Out.String(), "review.md") {
		t.Errorf("out=%q err=%q", h.Out.String(), h.Err.String())
	}
}

func TestVerdictShownWhenReportWriteFails(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.Reports, "stock", "pr-477-20261112-1030")
	// A directory where context.json goes makes report.Write fail.
	if err := os.MkdirAll(filepath.Join(dir, "context.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	code, err := h.run(context.Background(), "review", "stock", "477", "--mode", "ligero")
	if code != 1 || err == nil || !strings.Contains(err.Error(), "context.json") {
		t.Fatalf("code=%d err=%v, want 1 and the write error", code, err)
	}
	if !strings.Contains(h.Out.String(), "**Aprobar.**") {
		t.Errorf("the verdict must be shown even when the report cannot be written: %q", h.Out.String())
	}
}
