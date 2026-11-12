// Package repocache keeps a per-repo clone and creates per-review worktrees.
package repocache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/prb"
)

// ErrForkUnsupported is returned for PRs whose source lives in another repo.
var ErrForkUnsupported = errors.New("PR desde un fork: no soportado en la v1")

// Cache clones repos under Root and creates detached worktrees from them.
// CloneURL carries the {workspace} and {repo} placeholders.
type Cache struct {
	Exec     execx.Exec
	Root     string
	CloneURL string
}

// Worktrees are the checkouts of a PR's source (Head) and destination (Base).
type Worktrees struct{ Head, Base string }

// Prepare clones the repo if needed, fetches both branches and creates the
// head and base worktrees. The returned func removes them.
func (c Cache) Prepare(ctx context.Context, workspace, repo string, pr prb.PR) (Worktrees, func() error, error) {
	src, dst := pr.Source.Repository, pr.Destination.Repository
	if src != "" && dst != "" && src != dst {
		return Worktrees{}, nil, ErrForkUnsupported
	}

	repoDir := filepath.Join(c.Root, workspace, repo)
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Worktrees{}, nil, err
		}
		url := strings.NewReplacer("{workspace}", workspace, "{repo}", repo).Replace(c.CloneURL)
		if err := os.MkdirAll(filepath.Dir(repoDir), 0o755); err != nil {
			return Worktrees{}, nil, err
		}
		if _, err := c.git(ctx, "", "clone", "--quiet", url, repoDir); err != nil {
			return Worktrees{}, nil, fmt.Errorf("clonando %s/%s: %w", workspace, repo, err)
		}
	}

	if _, err := c.git(ctx, repoDir, "fetch", "--quiet", "origin", pr.Source.Branch, pr.Destination.Branch); err != nil {
		return Worktrees{}, nil, fmt.Errorf("fetch de %s/%s: %w", workspace, repo, err)
	}
	head, err := c.git(ctx, repoDir, "rev-parse", pr.Source.Commit+"^{commit}")
	if err != nil {
		return Worktrees{}, nil, fmt.Errorf("resolviendo commit origen %q: %w", pr.Source.Commit, err)
	}
	base, err := c.git(ctx, repoDir, "rev-parse", pr.Destination.Commit+"^{commit}")
	if err != nil {
		return Worktrees{}, nil, fmt.Errorf("resolviendo commit destino %q: %w", pr.Destination.Commit, err)
	}

	dir := filepath.Join(repoDir, "worktrees", fmt.Sprintf("pr%d-%s", pr.ID, pr.Source.Commit))
	wt := Worktrees{Head: filepath.Join(dir, "head"), Base: filepath.Join(dir, "base")}

	// Drop leftovers from an earlier run of the same PR/commit.
	for _, p := range []string{wt.Head, wt.Base} {
		_, _ = c.git(ctx, repoDir, "worktree", "remove", "--force", p)
	}
	if err := os.RemoveAll(dir); err != nil {
		return Worktrees{}, nil, err
	}
	_, _ = c.git(ctx, repoDir, "worktree", "prune")

	if _, err := c.git(ctx, repoDir, "worktree", "add", "--detach", wt.Head, head); err != nil {
		return Worktrees{}, nil, fmt.Errorf("worktree head: %w", err)
	}
	if _, err := c.git(ctx, repoDir, "worktree", "add", "--detach", wt.Base, base); err != nil {
		_, _ = c.git(ctx, repoDir, "worktree", "remove", "--force", wt.Head)
		_ = os.RemoveAll(dir)
		return Worktrees{}, nil, fmt.Errorf("worktree base: %w", err)
	}

	cleanup := func() error {
		var errs []error
		for _, p := range []string{wt.Head, wt.Base} {
			if _, err := c.git(ctx, repoDir, "worktree", "remove", "--force", p); err != nil {
				errs = append(errs, err)
			}
		}
		_ = os.Remove(dir)
		return errors.Join(errs...)
	}
	return wt, cleanup, nil
}

func (c Cache) git(ctx context.Context, dir string, args ...string) (string, error) {
	var out bytes.Buffer
	err := c.Exec(ctx, execx.Cmd{Name: "git", Args: args, Dir: dir, Stdout: &out})
	return strings.TrimSpace(out.String()), err
}
