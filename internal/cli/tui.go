package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/pr-review/internal/app"
	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/tui"
)

// uiFunc opens the terminal interface; tui.Run, replaced in tests.
type uiFunc func(ctx context.Context, d app.Deps, start tui.Start) error

// openUI is pr-review [repo]: the list of repo, or of the Bitbucket repo of
// the working copy, or the repo picker. Without a config yet, a bare repo
// name waits for the workspace the first-run setup asks for.
func openUI(cmd *cobra.Command, args []string, deps func() (app.Deps, error), ui uiFunc) error {
	d, err := deps()
	if err != nil {
		return err
	}
	var start tui.Start
	if len(args) == 1 {
		ref, err := app.ResolveRepo(args[0], d.Cfg.Workspace)
		switch {
		case err == nil:
			start.Repo = ref
		case d.ConfigFound:
			return usageError(err)
		default:
			start.Repo = args[0]
		}
	} else if dir, err := os.Getwd(); err == nil {
		start.Repo, _ = DetectRepo(d.Exec, dir)
	}
	return ui(cmd.Context(), d, start)
}

func newConfigCmd(deps func() (app.Deps, error), ui uiFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Cambia la configuración",
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "models",
		Short: "Abre la configuración de modelos",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			d, err := deps()
			if err != nil {
				return err
			}
			return ui(cmd.Context(), d, tui.Start{Models: true})
		},
	})
	return cmd
}

// noArgs rejects positional args, so a typo'd subcommand is a usage error.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError(fmt.Errorf("comando desconocido %q para %s", args[0], cmd.CommandPath()))
	}
	return nil
}

// DetectRepo returns the "workspace/repo" of the Bitbucket repo checked out
// in dir, from `git remote get-url origin`. It accepts
// git@bitbucket.org:ws/repo.git, https://[user@]bitbucket.org/ws/repo[.git]
// and ssh://git@bitbucket.org/ws/repo.git; anything else is not a
// Bitbucket checkout.
func DetectRepo(exec execx.Exec, dir string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out bytes.Buffer
	if err := exec(ctx, execx.Cmd{Name: "git", Args: []string{"remote", "get-url", "origin"}, Dir: dir, Stdout: &out}); err != nil {
		return "", false
	}
	return bitbucketRepo(strings.TrimSpace(out.String()))
}

func bitbucketRepo(remote string) (string, bool) {
	var p string
	if rest, ok := strings.CutPrefix(remote, "git@bitbucket.org:"); ok {
		p = rest
	} else {
		u, err := url.Parse(remote)
		if err != nil || u.Hostname() != "bitbucket.org" || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "ssh") {
			return "", false
		}
		p = strings.TrimPrefix(u.Path, "/")
	}
	p = strings.TrimSuffix(strings.TrimSuffix(p, "/"), ".git")
	ws, repo, ok := strings.Cut(p, "/")
	if !ok || ws == "" || repo == "" || strings.Contains(repo, "/") {
		return "", false
	}
	return ws + "/" + repo, true
}
