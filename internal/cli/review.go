package cli

import (
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
	_ = cmd.MarkFlagRequired("mode")
	return cmd
}

func runReview(cmd *cobra.Command, d Deps, repoArg string, n int, mode pipeline.Mode) error {
	ctx := cmd.Context()
	ref, err := ResolveRepo(repoArg, d.Cfg.Workspace)
	if err != nil {
		return usageError(err)
	}
	workspace, repo, _ := strings.Cut(ref, "/")

	now := d.Now
	if now == nil {
		now = time.Now
	}
	root := d.Cfg.ReportsDir
	if root == "" {
		root = report.ReportsRoot(d.Exec)
	}
	dir := report.Dir(root, repo, n, now())

	// The agents read the user's prompt overrides and log under the report.
	prompts.SetOverrideDir(d.ConfigDir)
	if r, ok := d.Agents.(agent.Runner); ok {
		r.LogDir = filepath.Join(dir, "agentes")
		d.Agents = r
	}

	pc, raw, err := d.PRB.Context(ctx, ref, n)
	if err != nil {
		return err
	}
	diff, err := d.PRB.Diff(ctx, ref, n, prechecks.ExcludeGlobs)
	if err != nil {
		return err
	}
	trees, cleanup, err := d.Cache.Prepare(ctx, workspace, repo, pc.PullRequest)
	if err != nil {
		return err
	}
	defer func() {
		if err := cleanup(); err != nil {
			fmt.Fprintln(d.Err, "aviso: no se pudieron borrar los worktrees:", err)
		}
	}()

	events := make(chan pipeline.Event)
	var printer sync.WaitGroup
	printer.Add(1)
	go func() {
		defer printer.Done()
		for ev := range events {
			printEvent(d.Err, ev)
		}
	}()
	res, err := pipeline.Run(ctx, pipeline.Deps{Cfg: d.Cfg, Agents: d.Agents, Now: d.Now},
		pipeline.Request{Repo: ref, PR: pc, Diff: diff, Trees: trees, Mode: mode}, events)
	close(events)
	printer.Wait()
	if err != nil {
		return err
	}

	if err := report.Write(dir, res, raw, diff); err != nil {
		return err
	}
	fmt.Fprintln(d.Out, report.VerdictLine(res))
	fmt.Fprintln(d.Out, filepath.Join(dir, "review.md"))
	if code := ExitCode(res.Verdict); code != 0 {
		return &exitError{Code: code}
	}
	return nil
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
