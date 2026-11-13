package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LeanMendez/pr-review/internal/agent"
	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/repocache"
)

// Deps are the collaborators of the commands, replaceable in tests.
type Deps struct {
	Cfg config.Config
	// ConfigDir holds the user's prompt overrides; empty uses the embedded ones.
	ConfigDir string
	PRB       prb.Client
	Cache     repocache.Cache
	Agents    pipeline.AgentRunner
	Exec      execx.Exec
	Now       func() time.Time
	Out       io.Writer
	Err       io.Writer
}

// DefaultDeps builds the real collaborators from the user's config. A missing
// config file means the defaults, with the workspace from BITBUCKET_WORKSPACE
// (see ResolveRepo). An invalid config is an error.
func DefaultDeps() (Deps, error) {
	path, err := config.Path()
	if err != nil {
		return Deps{}, fmt.Errorf("ubicando la configuración: %w", err)
	}
	cfg, _, err := config.Load(path)
	if err != nil {
		return Deps{}, err
	}
	if cfg.CacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return Deps{}, fmt.Errorf("ubicando la caché: %w", err)
		}
		cfg.CacheDir = filepath.Join(base, "pr-review")
	}
	ex := execx.Real()
	return Deps{
		Cfg:       cfg,
		ConfigDir: filepath.Dir(path),
		PRB:       prb.Client{Exec: ex},
		Cache:     repocache.Cache{Exec: ex, Root: filepath.Join(cfg.CacheDir, "repos"), CloneURL: cfg.CloneURL},
		Agents: agent.Runner{
			Exec:    ex,
			Bin:     "claude",
			Timeout: cfg.AgentTimeout,
			Sem:     make(chan struct{}, max(1, cfg.Concurrency)),
		},
		Exec: ex,
		Now:  time.Now,
		Out:  os.Stdout,
		Err:  os.Stderr,
	}, nil
}
