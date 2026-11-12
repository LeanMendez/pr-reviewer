package eligibility

import (
	"testing"

	"github.com/LeanMendez/pr-review/internal/prb"
)

func pr(mod func(*prb.PRContext)) prb.PRContext {
	c := prb.PRContext{
		PullRequest: prb.PR{ID: 1, Author: "otro"},
		Checks:      &prb.Checks{Overall: "pass"},
		Tasks:       &prb.Tasks{},
	}
	if mod != nil {
		mod(&c)
	}
	return c
}

func TestClassify(t *testing.T) {
	me := prb.Identity{Authenticated: true, User: "yo", AccountID: "acc-me"}
	tests := []struct {
		name   string
		c      prb.PRContext
		me     prb.Identity
		ready  bool
		reason Reason
	}{
		{"todo bien", pr(nil), me, true, ""},
		{"borrador", pr(func(c *prb.PRContext) { c.PullRequest.Draft = true }), me, false, ReasonDraft},
		{"borrador gana sobre CI", pr(func(c *prb.PRContext) {
			c.PullRequest.Draft = true
			c.Checks.Overall = "fail"
		}), me, false, ReasonDraft},
		{"ci fail", pr(func(c *prb.PRContext) { c.Checks.Overall = "fail" }), me, false, ReasonCIFailing},
		{"ci stopped", pr(func(c *prb.PRContext) { c.Checks.Overall = "stopped" }), me, false, ReasonCIFailing},
		{"ci pending", pr(func(c *prb.PRContext) { c.Checks.Overall = "pending" }), me, false, ReasonCIPending},
		{"ci nil", pr(func(c *prb.PRContext) { c.Checks = nil }), me, false, ReasonCIUnknown},
		{"ci none es listo", pr(func(c *prb.PRContext) { c.Checks.Overall = "none" }), me, true, ""},
		{"tareas sin resolver", pr(func(c *prb.PRContext) { c.Tasks.Unresolved = 2 }), me, false, ReasonTasks},
		{"tasks nil cuenta como 0", pr(func(c *prb.PRContext) { c.Tasks = nil }), me, true, ""},
		{"cambios pedidos", pr(func(c *prb.PRContext) {
			c.PullRequest.Review.Reviewers = []prb.Reviewer{{AccountID: "x", State: "changes_requested"}}
		}), me, false, ReasonChanges},
		{"mio", pr(func(c *prb.PRContext) { c.PullRequest.Author = "yo" }), me, false, ReasonMine},
		{"aprobado por mi", pr(func(c *prb.PRContext) {
			c.PullRequest.Review.Reviewers = []prb.Reviewer{{AccountID: "acc-me", State: "approved"}}
		}), me, false, ReasonApproved},
		{"aprobado por otro es listo", pr(func(c *prb.PRContext) {
			c.PullRequest.Review.Reviewers = []prb.Reviewer{{AccountID: "otro", State: "approved"}}
		}), me, true, ""},
		{"me vacio: nunca mio", pr(func(c *prb.PRContext) { c.PullRequest.Author = "" }), prb.Identity{}, true, ""},
		{"me vacio: nunca aprobado", pr(func(c *prb.PRContext) {
			c.PullRequest.Review.Reviewers = []prb.Reviewer{{AccountID: "", State: "approved"}}
		}), prb.Identity{}, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready, reason := Classify(tt.c, tt.me)
			if ready != tt.ready || reason != tt.reason {
				t.Fatalf("Classify = (%v, %q), want (%v, %q)", ready, reason, tt.ready, tt.reason)
			}
		})
	}
}

func TestSplit(t *testing.T) {
	draft := pr(func(c *prb.PRContext) { c.PullRequest.ID = 2; c.PullRequest.Draft = true })
	ok := pr(func(c *prb.PRContext) { c.PullRequest.ID = 3 })
	ready, hidden := Split([]prb.PRContext{draft, ok, draft}, prb.Identity{})
	if len(ready) != 1 || ready[0].PullRequest.ID != 3 {
		t.Fatalf("ready = %+v", ready)
	}
	if hidden[ReasonDraft] != 2 || len(hidden) != 1 {
		t.Fatalf("hidden = %v", hidden)
	}
}

func TestHiddenLine(t *testing.T) {
	tests := []struct {
		name   string
		hidden map[Reason]int
		want   string
	}{
		{"plural", map[Reason]int{ReasonChanges: 1, ReasonDraft: 2, ReasonCIFailing: 1},
			"4 PRs no aparecen: 2 en borrador, 1 con CI fallando, 1 con cambios pedidos."},
		{"singular", map[Reason]int{ReasonMine: 1}, "1 PR no aparece: 1 tuyos."},
		{"vacio", map[Reason]int{}, ""},
		{"nil", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HiddenLine(tt.hidden); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
