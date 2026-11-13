// Package app runs a review the way every front end of pr-review does: the
// collaborators (Deps) and the review sequence (Review), shared by the
// command line and the terminal interface.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LeanMendez/pr-review/internal/agent"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prechecks"
	"github.com/LeanMendez/pr-review/internal/prompts"
	"github.com/LeanMendez/pr-review/internal/report"
)

// ResolveRepo returns the "workspace/repo" form of arg. A bare repo name takes
// workspace, or BITBUCKET_WORKSPACE when workspace is empty.
func ResolveRepo(arg, workspace string) (string, error) {
	if strings.Contains(arg, "/") {
		return arg, nil
	}
	if workspace == "" {
		workspace = os.Getenv("BITBUCKET_WORKSPACE")
	}
	if workspace == "" {
		return "", errors.New("Falta el workspace: configuralo con pr-review o pasá workspace/repo.")
	}
	return workspace + "/" + arg, nil
}

// CleanupError is a review that finished, with its report written, whose
// worktrees could not be removed: a warning, not a failed review.
type CleanupError struct{ Err error }

func (e *CleanupError) Error() string { return "no se pudieron borrar los worktrees: " + e.Err.Error() }

func (e *CleanupError) Unwrap() error { return e.Err }

// Review runs one review the way the review command does: the PR context
// and its diff, the worktrees (removed on the way out), the pipeline and
// the report. repo is "workspace/repo" or a bare name for the configured
// workspace. The report dir is created private before any agent runs. It
// prints nothing: progress goes to events (when not nil), which it never
// closes. It returns the result and the report dir.
//
// When the review finished but the report could not be written, the result
// and the dir come back with the error, so the caller can still show the
// verdict. When only the worktree cleanup failed, the error is a
// *CleanupError and the result and the report are complete.
func Review(ctx context.Context, d Deps, repo string, n int, mode pipeline.Mode, events chan<- pipeline.Event) (res pipeline.Result, dir string, err error) {
	ref, err := ResolveRepo(repo, d.Cfg.Workspace)
	if err != nil {
		return pipeline.Result{}, "", err
	}
	workspace, name, _ := strings.Cut(ref, "/")

	now := d.Now
	if now == nil {
		now = time.Now
	}
	root := d.Cfg.ReportsDir
	if root == "" {
		root = report.ReportsRoot(d.Exec)
	}
	dir = report.Dir(root, name, n, now())

	// The agents read the user's prompt overrides and log under the report.
	prompts.SetOverrideDir(d.ConfigDir)
	if r, ok := d.Agents.(agent.Runner); ok {
		r.LogDir = filepath.Join(dir, "agentes")
		d.Agents = r
	}

	pc, raw, err := d.PRB.Context(ctx, ref, n)
	if err != nil {
		return pipeline.Result{}, "", err
	}
	diff, err := d.PRB.Diff(ctx, ref, n, prechecks.ExcludeGlobs)
	if err != nil {
		return pipeline.Result{}, "", err
	}
	trees, cleanup, err := d.Cache.Prepare(ctx, workspace, name, pc.PullRequest)
	if err != nil {
		return pipeline.Result{}, "", err
	}
	defer func() {
		switch cerr := cleanup(); {
		case cerr == nil:
		case err == nil:
			err = &CleanupError{Err: cerr}
		default:
			err = errors.Join(err, fmt.Errorf("no se pudieron borrar los worktrees: %w", cerr))
		}
	}()

	// The report dir is private before any agent logs into it.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return pipeline.Result{}, "", fmt.Errorf("crear %s: %w", dir, err)
	}
	res, err = pipeline.Run(ctx, pipeline.Deps{Cfg: d.Cfg, Agents: d.Agents, Now: now},
		pipeline.Request{Repo: ref, PR: pc, Diff: diff, Trees: trees, Mode: mode}, events)
	if err != nil {
		_ = os.Remove(dir) // only when empty: agent logs stay
		return pipeline.Result{}, "", err
	}
	if err := report.Write(dir, res, raw, diff); err != nil {
		return res, dir, err
	}
	return res, dir, nil
}
