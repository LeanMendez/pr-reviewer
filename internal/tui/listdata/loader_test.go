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
