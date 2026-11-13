package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/pr-review/internal/app"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/pipeline"
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

func newReviewCmd(deps func() (app.Deps, error)) *cobra.Command {
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

func runReview(cmd *cobra.Command, d app.Deps, repoArg string, n int, mode pipeline.Mode) error {
	ref, err := app.ResolveRepo(repoArg, d.Cfg.Workspace)
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
	res, dir, err := app.Review(cmd.Context(), d, ref, n, mode, events)
	close(events)
	printer.Wait()
	var cleanupErr *app.CleanupError
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
