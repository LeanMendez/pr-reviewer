// Package apptest runs app.Review in tests without prb, git or claude: prb
// answers with one small PR, git only pretends to manage worktrees and the
// agents return canned reviews.
package apptest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LeanMendez/pr-review/internal/agent"
	"github.com/LeanMendez/pr-review/internal/app"
	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/prompts"
	"github.com/LeanMendez/pr-review/internal/repocache"
)

// Agents answers by role with an empty, clean review.
type Agents struct {
	mu    sync.Mutex
	specs []agent.Spec
	// OnRun, when set, runs first and may fail the agent.
	OnRun func(context.Context, agent.Spec) error
	// Critical makes bugs report a critico finding that the validator confirms.
	Critical bool
}

// Specs returns the agents run so far.
func (f *Agents) Specs() []agent.Spec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agent.Spec(nil), f.specs...)
}

// Run records s and returns the canned output of its role.
func (f *Agents) Run(ctx context.Context, s agent.Spec, _ func(agent.Activity)) (json.RawMessage, error) {
	f.mu.Lock()
	f.specs = append(f.specs, s)
	f.mu.Unlock()
	if f.OnRun != nil {
		if err := f.OnRun(ctx, s); err != nil {
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
		if f.Critical {
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

// ContextJSON is what `prb pr context` returns: PR 477 of ws/stock.
const ContextJSON = `{"repository":"ws/stock","pull_request":{"id":477,"title":"CU-1 Pagos","state":"OPEN",
"source":{"branch":"f","commit":"abc","repository":"ws/stock"},"destination":{"branch":"main","commit":"def","repository":"ws/stock"}},
"files":{"count":1,"lines_added":1,"lines_removed":0,"files":[{"path":"src/a.go","status":"modified","lines_added":1,"lines_removed":0}]}}`

// DiffJSON is what `prb pr diff` returns for that PR.
const DiffJSON = `{"total_files":1,"files":[{"path":"src/a.go","status":"modified","lines_added":1,"lines_removed":0,
"patch":"@@ -1,1 +1,2 @@\n x\n+y\n"}]}`

// Harness holds Deps whose prb, git and agents are fakes, with the config
// for workspace "ws", reports under Reports and the clock at
// 2026-11-12 10:30 UTC.
type Harness struct {
	Deps    app.Deps
	Out     bytes.Buffer
	Err     bytes.Buffer
	Reports string
	Agents  *Agents

	mu      sync.Mutex
	removed int
	calls   []string
}

// New builds a Harness with its own temporary reports and cache dirs.
func New(t testing.TB) *Harness {
	t.Helper()
	h := &Harness{Reports: t.TempDir(), Agents: &Agents{}}
	cfg := config.Defaults()
	cfg.Workspace = "ws"
	cfg.ReportsDir = h.Reports
	ex := execx.Fake(func(c execx.Cmd) (string, string, int) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.calls = append(h.calls, c.Name+" "+strings.Join(c.Args, " "))
		switch c.Name {
		case "prb":
			if len(c.Args) > 1 && c.Args[1] == "context" {
				return ContextJSON, "", 0
			}
			return DiffJSON, "", 0
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
	h.Deps = app.Deps{
		Cfg:    cfg,
		PRB:    prb.Client{Exec: ex},
		Cache:  repocache.Cache{Exec: ex, Root: t.TempDir(), CloneURL: cfg.CloneURL},
		Agents: h.Agents,
		Exec:   ex,
		Now:    func() time.Time { return time.Date(2026, 11, 12, 10, 30, 0, 0, time.UTC) },
		Out:    &h.Out,
		Err:    &h.Err,
	}
	return h
}

// Removed is how many worktrees the review removed.
func (h *Harness) Removed() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.removed
}

// Calls lists the commands run so far, as "name args...".
func (h *Harness) Calls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}
