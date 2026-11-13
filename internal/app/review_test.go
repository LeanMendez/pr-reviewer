package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/LeanMendez/pr-review/internal/app"
	"github.com/LeanMendez/pr-review/internal/app/apptest"
	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/pipeline"
)

func TestReviewDirect(t *testing.T) {
	h := apptest.New(t)
	events := make(chan pipeline.Event, 256)
	res, dir, err := app.Review(context.Background(), h.Deps, "stock", 477, pipeline.ModeLigero, events)
	close(events)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(h.Reports, "stock", "pr-477-20261112-1030"); dir != want {
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
	if h.Out.Len() != 0 || h.Err.Len() != 0 {
		t.Errorf("Review printed: out=%q err=%q", h.Out.String(), h.Err.String())
	}
	if h.Removed() != 2 {
		t.Errorf("worktrees removed = %d, want 2", h.Removed())
	}
}

func TestReviewCleanupFailureIsAWarning(t *testing.T) {
	h := apptest.New(t)
	inner := h.Deps.Cache.Exec
	h.Deps.Cache.Exec = func(ctx context.Context, c execx.Cmd) error {
		if c.Name == "git" && len(c.Args) > 1 && c.Args[0] == "worktree" && c.Args[1] == "remove" {
			return errors.New("worktree ocupado")
		}
		return inner(ctx, c)
	}
	res, _, err := app.Review(context.Background(), h.Deps, "stock", 477, pipeline.ModeLigero, nil)
	var ce *app.CleanupError
	if !errors.As(err, &ce) || res.Verdict != finding.VerdictAprobar {
		t.Fatalf("err = %v (%T), verdict %q: want a *CleanupError with the full result", err, err, res.Verdict)
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
		got, err := app.ResolveRepo(c.arg, c.ws)
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
	if got, err := app.ResolveRepo("stock", ""); err != nil || got != "envws/stock" {
		t.Errorf("env: %q %v", got, err)
	}
}

func TestWorkspace(t *testing.T) {
	t.Setenv("BITBUCKET_WORKSPACE", "")
	if ws, err := app.Workspace("ws"); err != nil || ws != "ws" {
		t.Errorf("configured: %q %v", ws, err)
	}
	if _, err := app.Workspace(""); err == nil || err.Error() != "Falta el workspace: configuralo con pr-review o pasá workspace/repo." {
		t.Errorf("missing: err = %v", err)
	}
	t.Setenv("BITBUCKET_WORKSPACE", "envws")
	if ws, err := app.Workspace(""); err != nil || ws != "envws" {
		t.Errorf("env: %q %v", ws, err)
	}
}

func TestReportsRoot(t *testing.T) {
	h := apptest.New(t)
	if got := app.ReportsRoot(h.Deps); got != h.Reports {
		t.Errorf("configured: %q, want %q", got, h.Reports)
	}
	h.Deps.Cfg.ReportsDir = ""
	h.Deps.Exec = execx.Fake(func(c execx.Cmd) (string, string, int) {
		if c.Name == "xdg-user-dir" {
			return "/home/x/Docs\n", "", 0
		}
		return "", "", 1
	})
	if got := app.ReportsRoot(h.Deps); got != "/home/x/Docs/reviews" {
		t.Errorf("from xdg-user-dir: %q", got)
	}
}
