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
	"sync"

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

// repoLocks serializes the git work on each cached repo (clone, fetch,
// worktree add and remove) between the reviews of this process: one
// channel of capacity 1 per repo dir.
var repoLocks sync.Map

// lockRepo waits for the repo at dir, or for ctx, and returns the unlock.
func lockRepo(ctx context.Context, dir string) (func(), error) {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	v, _ := repoLocks.LoadOrStore(dir, make(chan struct{}, 1))
	lock := v.(chan struct{})
	select {
	case lock <- struct{}{}:
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Prepare clones the repo if needed, fetches both branches and creates the
// head and base worktrees in a directory of their own, so concurrent
// reviews, even of the same PR and commit, never share or remove each
// other's. The returned func removes them.
func (c Cache) Prepare(ctx context.Context, workspace, repo string, pr prb.PR) (Worktrees, func() error, error) {
	src, dst := pr.Source.Repository, pr.Destination.Repository
	if src != "" && dst != "" && src != dst {
		return Worktrees{}, nil, ErrForkUnsupported
	}

	for _, name := range []string{workspace, repo} {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return Worktrees{}, nil, fmt.Errorf("nombre inválido %q: no puede estar vacío ni contener separadores ni '..'", name)
		}
	}
	base0 := filepath.Join(c.Root, workspace, repo)
	repoDir := filepath.Join(base0, "clone")
	if err := c.contained(repoDir); err != nil {
		return Worktrees{}, nil, err
	}
	unlock, err := lockRepo(ctx, base0)
	if err != nil {
		return Worktrees{}, nil, err
	}
	defer unlock()
	url := strings.NewReplacer("{workspace}", workspace, "{repo}", repo).Replace(c.CloneURL)
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Worktrees{}, nil, err
		}
		if err := os.MkdirAll(filepath.Dir(repoDir), 0o755); err != nil {
			return Worktrees{}, nil, err
		}
		if _, err := c.git(ctx, "", "clone", "--quiet", url, repoDir); err != nil {
			return Worktrees{}, nil, fmt.Errorf("clonando %s/%s: %w", workspace, repo, err)
		}
	} else if _, err := c.git(ctx, repoDir, "remote", "set-url", "origin", url); err != nil {
		// A clone keeps the origin it was made from: follow the configured
		// URL, so a moved host or an edited config takes effect.
		return Worktrees{}, nil, fmt.Errorf("actualizando origin de %s/%s: %w", workspace, repo, err)
	}

	// Branch names come from the PR: as full refs, none can read as an option.
	if _, err := c.git(ctx, repoDir, "fetch", "--quiet", "origin", "refs/heads/"+pr.Source.Branch, "refs/heads/"+pr.Destination.Branch); err != nil {
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

	parent := filepath.Join(base0, "worktrees")
	if err := c.contained(parent); err != nil {
		return Worktrees{}, nil, err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Worktrees{}, nil, err
	}
	dir, err := os.MkdirTemp(parent, fmt.Sprintf("pr%d-%s-", pr.ID, head[:min(12, len(head))]))
	if err != nil {
		return Worktrees{}, nil, err
	}
	wt := Worktrees{Head: filepath.Join(dir, "head"), Base: filepath.Join(dir, "base")}

	// Forget worktrees whose directories are gone.
	_, _ = c.git(ctx, repoDir, "worktree", "prune")

	if _, err := c.git(ctx, repoDir, "worktree", "add", "--detach", wt.Head, head); err != nil {
		_ = os.RemoveAll(dir)
		return Worktrees{}, nil, fmt.Errorf("worktree head: %w", err)
	}
	if _, err := c.git(ctx, repoDir, "worktree", "add", "--detach", wt.Base, base); err != nil {
		_, _ = c.git(ctx, repoDir, "worktree", "remove", "--force", wt.Head)
		_ = os.RemoveAll(dir)
		return Worktrees{}, nil, fmt.Errorf("worktree base: %w", err)
	}

	cleanup := func() error {
		// Review contexts get cancelled on quit; cleanup must still run.
		ctx := context.WithoutCancel(ctx)
		unlock, _ := lockRepo(ctx, base0) // never fails without a cancel
		defer unlock()
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

// contained fails unless p is strictly under c.Root.
func (c Cache) contained(p string) error {
	rel, err := filepath.Rel(c.Root, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("ruta fuera de la caché: %s", p)
	}
	return nil
}
