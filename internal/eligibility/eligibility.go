// Package eligibility decides which PRs are ready to appear in the review list.
package eligibility

import (
	"fmt"
	"strings"

	"github.com/LeanMendez/pr-review/internal/prb"
)

// Reason says why a PR is hidden from the list.
type Reason string

const (
	ReasonDraft     Reason = "en borrador"
	ReasonCIFailing Reason = "con CI fallando"
	ReasonCIPending Reason = "con CI en curso"
	ReasonCIUnknown Reason = "con CI sin datos"
	ReasonTasks     Reason = "con tareas sin resolver"
	ReasonChanges   Reason = "con cambios pedidos"
	ReasonMine      Reason = "tuyos"
	ReasonApproved  Reason = "ya aprobados por vos"
)

// order is the display order of reasons in HiddenLine.
var order = []Reason{
	ReasonDraft, ReasonCIFailing, ReasonCIPending, ReasonCIUnknown,
	ReasonTasks, ReasonChanges, ReasonMine, ReasonApproved,
}

// Classify reports whether the PR is ready; if not, the first reason that applies.
// A nil Tasks section counts as zero unresolved tasks. CI counts as passed
// only for "pass" and "none" (no pipelines); any other or missing status is
// unknown, never ready.
func Classify(c prb.PRContext, me prb.Identity) (bool, Reason) {
	if c.PullRequest.Draft {
		return false, ReasonDraft
	}
	if c.Checks == nil {
		return false, ReasonCIUnknown
	}
	switch c.Checks.Overall {
	case "pass", "none":
	case "fail", "stopped":
		return false, ReasonCIFailing
	case "pending":
		return false, ReasonCIPending
	default:
		return false, ReasonCIUnknown
	}
	if c.Tasks != nil && c.Tasks.Unresolved > 0 {
		return false, ReasonTasks
	}
	for _, r := range c.PullRequest.Review.Reviewers {
		if r.State == "changes_requested" {
			return false, ReasonChanges
		}
	}
	if me.User != "" && c.PullRequest.Author == me.User {
		return false, ReasonMine
	}
	if me.AccountID != "" {
		for _, r := range c.PullRequest.Review.Reviewers {
			if r.AccountID == me.AccountID && r.State == "approved" {
				return false, ReasonApproved
			}
		}
	}
	return true, ""
}

// Split separates ready PRs from hidden ones, counting hidden ones by reason.
func Split(cs []prb.PRContext, me prb.Identity) ([]prb.PRContext, map[Reason]int) {
	var ready []prb.PRContext
	hidden := map[Reason]int{}
	for _, c := range cs {
		if ok, reason := Classify(c, me); ok {
			ready = append(ready, c)
		} else {
			hidden[reason]++
		}
	}
	return ready, hidden
}

// HiddenLine builds the summary line for hidden PRs, or "" if none are hidden.
func HiddenLine(hidden map[Reason]int) string {
	total := 0
	var parts []string
	for _, r := range order {
		if n := hidden[r]; n > 0 {
			total += n
			parts = append(parts, fmt.Sprintf("%d %s", n, r))
		}
	}
	if total == 0 {
		return ""
	}
	head := fmt.Sprintf("%d PRs no aparecen", total)
	if total == 1 {
		head = "1 PR no aparece"
	}
	return head + ": " + strings.Join(parts, ", ") + "."
}
