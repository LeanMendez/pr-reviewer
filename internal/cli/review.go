package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/pr-review/internal/agent"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prechecks"
	"github.com/LeanMendez/pr-review/internal/prompts"
	"github.com/LeanMendez/pr-review/internal/report"
)

const errBadMode = "Modo inválido: usá ligero, exhaustivo o judgment."

// ExitCode maps a verdict to the process exit code. Errors are 1 and usage
// errors 2.
func ExitCode(v finding.Verdict) int {
	switch v {
	case finding.VerdictAprobar:
		return 0
	case finding.VerdictAprobarConComentarios:
		return 10
	case finding.VerdictPedirCambios:
		return 20
	case finding.VerdictRevisarAMano:
		return 30
	}
	return 1
}

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

func newReviewCmd(deps func() (Deps, error)) *cobra.Command {
	var mode string
	cmd := &cobra.Command{
		Use:   "review <repo> <pr> --mode ligero|exhaustivo|judgment",
		Short: "Revisa un PR sin interacción y escribe el reporte",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return usageError(errors.New("uso: pr-review review <repo> <pr> --mode ligero|exhaustivo|judgment"))
			}
			if n, err := strconv.Atoi(args[1]); err != nil || n <= 0 {
				return usageError(fmt.Errorf("PR inválido %q: tiene que ser un número", args[1]))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// A missing --mode is an empty one: ParseMode rejects it.
			m, err := pipeline.ParseMode(mode)
			if err != nil {
				return usageError(errors.New(errBadMode))
			}
			n, _ := strconv.Atoi(args[1])
			d, err := deps()
			if err != nil {
				return err
			}
			return runReview(cmd, d, args[0], n, m)
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "", "modo de revisión: ligero, exhaustivo o judgment")
	return cmd
}

func runReview(cmd *cobra.Command, d Deps, repoArg string, n int, mode pipeline.Mode) error {
	ref, err := ResolveRepo(repoArg, d.Cfg.Workspace)
	if err != nil {
		return usageError(err)
	}
	events := make(chan pipeline.Event, 64)
	var printer sync.WaitGroup
	printer.Add(1)
	go func() {
		defer printer.Done()
		for ev := range events {
			printEvent(d.Err, ev)
		}
	}()
	res, dir, err := Review(cmd.Context(), d, ref, n, mode, events)
	close(events)
	printer.Wait()
	var cleanupErr *CleanupError
	if errors.As(err, &cleanupErr) {
		fmt.Fprintln(d.Err, "aviso: no se pudieron borrar los worktrees:", cleanupErr.Err)
		err = nil
	}
	// The verdict first: a report that cannot be written must not hide
	// the outcome of a long review.
	if res.Verdict != "" {
		fmt.Fprintln(d.Out, report.VerdictLine(res))
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(d.Out, filepath.Join(dir, "review.md"))
	code, err := verdictResult(res.Verdict)
	if err != nil {
		return err
	}
	if code != 0 {
		return &exitError{Code: code}
	}
	return nil
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

// printEvent writes one progress line per event.
func printEvent(w interface{ Write([]byte) (int, error) }, ev pipeline.Event) {
	stage := fmt.Sprintf("%d %s", int(ev.Stage), ev.Stage.Name())
	switch ev.Kind {
	case pipeline.StageStarted:
		fmt.Fprintf(w, "▸ %s\n", stage)
	case pipeline.StageDone:
		if ev.Candidates > 0 {
			fmt.Fprintf(w, "✓ %s (%d candidatos)\n", stage, ev.Candidates)
		} else {
			fmt.Fprintf(w, "✓ %s\n", stage)
		}
	case pipeline.StageSkipped:
		fmt.Fprintf(w, "- %s (omitida)\n", stage)
	case pipeline.AgentStarted:
		fmt.Fprintf(w, "  · %s (%s)\n", ev.Agent, ev.Model)
	case pipeline.AgentActivity:
		fmt.Fprintf(w, "    %s: %s\n", ev.Agent, ev.Text)
	case pipeline.AgentDone:
		fmt.Fprintf(w, "  ✓ %s\n", ev.Agent)
	case pipeline.AgentFailed:
		fmt.Fprintf(w, "  ✗ %s: %v\n", ev.Agent, ev.Err)
	}
}

// verdictResult is the exit code of a verdict. An unknown verdict is an error
// with a message, so the process never exits 1 silently.
func verdictResult(v finding.Verdict) (int, error) {
	switch v {
	case finding.VerdictAprobar, finding.VerdictAprobarConComentarios, finding.VerdictPedirCambios, finding.VerdictRevisarAMano:
		return ExitCode(v), nil
	}
	return 1, fmt.Errorf("veredicto desconocido: %s", v)
}
