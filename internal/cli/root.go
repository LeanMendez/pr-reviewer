// Package cli define los comandos de pr-review.
package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version se pisa en el build con -ldflags si hace falta.
var version = "0.1.0"

// NewRootCmd arma el comando raíz de pr-review.
func NewRootCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "pr-review",
		Short:        "Revisa PRs de Bitbucket con varios agentes de Claude",
		Version:      version,
		SilenceUsage: true,
	}
	cmd.SetVersionTemplate("pr-review {{.Version}}\n")
	return cmd
}

// Execute corre el comando raíz y devuelve el código de salida del proceso.
func Execute(ctx context.Context) int {
	cmd := NewRootCmd(version)
	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}
