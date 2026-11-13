// Package cli define los comandos de pr-review.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/pr-review/internal/app"
	"github.com/LeanMendez/pr-review/internal/tui"
)

// version se pisa en el build con -ldflags si hace falta.
var version = "0.1.0"

// exitError lleva el código de salida de un comando. Err vacío significa que
// no hay nada que imprimir (los veredictos).
type exitError struct {
	Code int
	Err  error
}

func (e *exitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("salida con código %d", e.Code)
	}
	return e.Err.Error()
}

func (e *exitError) Unwrap() error { return e.Err }

// usageError marca un error de uso (código 2).
func usageError(err error) error { return &exitError{Code: 2, Err: err} }

// exitCodeOf devuelve el código de salida de err: 0 sin error, el del
// exitError si lo hay y 1 en cualquier otro caso.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	return 1
}

// handleError imprime err una sola vez en w, salvo que sea un código sin
// mensaje, y devuelve el código de salida.
func handleError(err error, w io.Writer) int {
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) && ee.Err == nil {
		return ee.Code
	}
	fmt.Fprintln(w, "error:", err)
	return exitCodeOf(err)
}

// NewRootCmd arma el comando raíz de pr-review con las dependencias reales y
// la interfaz de terminal.
func NewRootCmd(version string) *cobra.Command { return newRootCmd(version, app.DefaultDeps, tui.Run) }

func newRootCmd(version string, deps func() (app.Deps, error), ui uiFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "pr-review [repo]",
		Short:         "Revisa PRs de Bitbucket con varios agentes de Claude",
		Long:          "Revisa PRs de Bitbucket con varios agentes de Claude.\n\nSin argumento abre la interfaz con el repo de Bitbucket de la copia actual o, si no hay, con un selector de repos.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// One word is a repo name; more is a typo'd subcommand with its args.
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageError(fmt.Errorf("comando desconocido %q; para abrir la interfaz: pr-review [repo]", args[0]))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error { return openUI(cmd, args, deps, ui) },
	}
	cmd.SetVersionTemplate("pr-review {{.Version}}\n")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError(err) })
	cmd.AddCommand(newReviewCmd(deps), newConfigCmd(deps, ui))
	return cmd
}

// Execute corre el comando raíz y devuelve el código de salida del proceso.
func Execute(ctx context.Context) int {
	return handleError(NewRootCmd(version).ExecuteContext(ctx), os.Stderr)
}
