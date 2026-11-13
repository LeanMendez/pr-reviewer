package listdata

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/eligibility"
	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
)

var me = prb.Identity{User: "Yo", AccountID: "acc-me"}

func pr(id int, author string, draft bool, updated time.Time, files *prb.Files) prb.PRContext {
	return prb.PRContext{
		PullRequest: prb.PR{ID: id, Title: "t" + strconv.Itoa(id), Author: author, Draft: draft, UpdatedOn: updated},
		Checks:      &prb.Checks{Overall: "pass"},
		Files:       files,
	}
}

func files(lines ...int) *prb.Files {
	f := &prb.Files{}
	for i, n := range lines {
		f.Files = append(f.Files, prb.FileStat{Path: "f" + strconv.Itoa(i) + ".go", Status: "modified", LinesAdded: n})
	}
	return f
}

// loader answers `pr list` and `pr context <n>` from the given contexts.
func loader(t *testing.T, root string, ctxs ...prb.PRContext) Loader {
	t.Helper()
	byID := map[string]prb.PRContext{}
	var list []prb.PRSummary
	for _, c := range ctxs {
		byID[strconv.Itoa(c.PullRequest.ID)] = c
		list = append(list, prb.PRSummary{ID: c.PullRequest.ID, Title: c.PullRequest.Title})
	}
	exec := execx.Fake(func(c execx.Cmd) (string, string, int) {
		if len(c.Args) >= 3 && c.Args[0] == "pr" && c.Args[1] == "list" {
			b, _ := json.Marshal(list)
			return string(b), "", 0
		}
		if len(c.Args) >= 3 && c.Args[0] == "pr" && c.Args[1] == "context" {
			b, _ := json.Marshal(byID[c.Args[2]])
			return string(b), "", 0
		}
		return "", "unexpected " + c.Name, 1
	})
	return Loader{PRB: prb.Client{Exec: exec}, Me: me, ReportsRoot: root, Cfg: config.Defaults()}
}

func TestLoadSplitsReadyAndHidden(t *testing.T) {
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	l := loader(t, t.TempDir(),
		pr(1, "Ana", false, t0, files(20)),
		pr(2, "Ana", true, t0.Add(time.Hour), files(10)),
		pr(3, "Bruno", false, t0.Add(2*time.Hour), files(700)),
	)
	ready, hidden, err := l.Load(context.Background(), "ws/stock")
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 2 || ready[0].Ctx.PullRequest.ID != 3 || ready[1].Ctx.PullRequest.ID != 1 {
		t.Fatalf("ready must be newest first, got %+v", ready)
	}
	if ready[0].Lines != 700 || ready[0].Suggested != pipeline.ModeExhaustivo || ready[0].Reason != "" {
		t.Errorf("big PR row = %+v", ready[0])
	}
	if ready[1].Lines != 20 || ready[1].Suggested != pipeline.ModeLigero {
		t.Errorf("small PR row = %+v", ready[1])
	}
	if len(hidden) != 1 || hidden[0].Ctx.PullRequest.ID != 2 || hidden[0].Reason != eligibility.ReasonDraft {
		t.Errorf("hidden = %+v", hidden)
	}
}

func TestLoadNilFilesDoesNotPanic(t *testing.T) {
	l := loader(t, t.TempDir(), pr(1, "Ana", false, time.Now(), nil))
	ready, _, err := l.Load(context.Background(), "ws/stock")
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].Lines != 0 || ready[0].Suggested != pipeline.ModeLigero {
		t.Errorf("rows = %+v", ready)
	}
}

func TestLoadMarksReviewed(t *testing.T) {
	root := t.TempDir()
	mk := func(name string, findings bool) {
		dir := filepath.Join(root, "stock", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if findings {
			if err := os.WriteFile(filepath.Join(dir, "findings.json"), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("pr-477-20261112-0412", true)
	mk("pr-478-20261112-0500", false) // failed run: not reviewed
	t0 := time.Now()
	l := loader(t, root,
		pr(477, "Ana", false, t0.Add(time.Hour), files(5)),
		pr(478, "Ana", false, t0, files(5)),
	)
	ready, _, err := l.Load(context.Background(), "ws/stock")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 12, 4, 12, 0, 0, time.Local)
	if !ready[0].Reviewed || !ready[0].LastReview.Equal(want) {
		t.Errorf("477 = %+v", ready[0])
	}
	if ready[1].Reviewed || !ready[1].LastReview.IsZero() {
		t.Errorf("478 = %+v", ready[1])
	}
}

func TestLoadPropagatesPRBError(t *testing.T) {
	exec := execx.Fake(func(c execx.Cmd) (string, string, int) {
		return "", `{"error":{"code":"no_credentials","message":"sin credenciales"}}`, 4
	})
	l := Loader{PRB: prb.Client{Exec: exec}, Cfg: config.Defaults()}
	_, _, err := l.Load(context.Background(), "ws/stock")
	var pe *prb.Error
	if !errors.As(err, &pe) || pe.Code != "no_credentials" {
		t.Fatalf("err = %v, want *prb.Error no_credentials", err)
	}
}

// LoadPage asks prb for at most limit PRs, fetches the context only of the
// ones not in have, and says whether prb may have more.
func TestLoadPageSkipsLoadedAndReportsMore(t *testing.T) {
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	all := []prb.PRContext{
		pr(3, "Ana", false, t0.Add(3*time.Hour), files(10)),
		pr(2, "Beto", false, t0.Add(2*time.Hour), files(10)),
		pr(1, "Caro", false, t0.Add(time.Hour), files(10)),
	}
	var limits []string
	var contexts []string
	exec := execx.Fake(func(c execx.Cmd) (string, string, int) {
		if len(c.Args) >= 2 && c.Args[0] == "pr" && c.Args[1] == "list" {
			n := len(all)
			for i, a := range c.Args {
				if a == "--limit" && i+1 < len(c.Args) {
					limits = append(limits, c.Args[i+1])
					if v, _ := strconv.Atoi(c.Args[i+1]); v > 0 && v < n {
						n = v
					}
				}
			}
			var list []prb.PRSummary
			for _, p := range all[:n] {
				list = append(list, prb.PRSummary{ID: p.PullRequest.ID})
			}
			b, _ := json.Marshal(list)
			return string(b), "", 0
		}
		if len(c.Args) >= 3 && c.Args[0] == "pr" && c.Args[1] == "context" {
			contexts = append(contexts, c.Args[2])
			for _, p := range all {
				if strconv.Itoa(p.PullRequest.ID) == c.Args[2] {
					b, _ := json.Marshal(p)
					return string(b), "", 0
				}
			}
		}
		return "", "unexpected", 1
	})
	l := Loader{PRB: prb.Client{Exec: exec}, Me: me, ReportsRoot: t.TempDir(), Cfg: config.Defaults()}

	ready, hidden, more, err := l.LoadPage(context.Background(), "ws/repo", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 2 || len(hidden) != 0 || ready[0].Ctx.PullRequest.ID != 3 || ready[1].Ctx.PullRequest.ID != 2 || !more {
		t.Fatalf("first page: ready=%d hidden=%d more=%v", len(ready), len(hidden), more)
	}

	contexts = nil
	ready, _, more, err = l.LoadPage(context.Background(), "ws/repo", 4, map[int]bool{3: true, 2: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].Ctx.PullRequest.ID != 1 || more {
		t.Fatalf("second page: ready=%+v more=%v", ready, more)
	}
	if len(contexts) != 1 || contexts[0] != "1" {
		t.Errorf("contexts fetched on the second page = %v, want only the new PR 1", contexts)
	}
	if len(limits) != 2 || limits[0] != "2" || limits[1] != "4" {
		t.Errorf("limits = %v, want [2 4]", limits)
	}
}
