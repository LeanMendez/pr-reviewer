package app_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LeanMendez/pr-review/internal/app"
)

func TestDefaultDepsReportsConfigFound(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	d, err := app.DefaultDeps()
	if err != nil || d.ConfigFound {
		t.Fatalf("no file: found=%v err=%v", d.ConfigFound, err)
	}
	p := filepath.Join(cfgHome, "pr-review", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("workspace: ws\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if d, err = app.DefaultDeps(); err != nil || !d.ConfigFound || d.Cfg.Workspace != "ws" {
		t.Fatalf("with file: found=%v workspace=%q err=%v", d.ConfigFound, d.Cfg.Workspace, err)
	}
}

// TestDefaultDepsKeepsCacheDirOutOfConfig: the derived cache dir is only
// used, never put in the config, so saving the config does not pin it.
func TestDefaultDepsKeepsCacheDirOutOfConfig(t *testing.T) {
	cfgHome, cacheHome := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	d, err := app.DefaultDeps()
	if err != nil {
		t.Fatal(err)
	}
	if d.Cfg.CacheDir != "" || d.Cache.Root != filepath.Join(cacheHome, "pr-review", "repos") {
		t.Errorf("derived: Cfg.CacheDir=%q Cache.Root=%q", d.Cfg.CacheDir, d.Cache.Root)
	}
	p := filepath.Join(cfgHome, "pr-review", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("directorio_cache: /x/cache\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if d, err = app.DefaultDeps(); err != nil || d.Cfg.CacheDir != "/x/cache" || d.Cache.Root != "/x/cache/repos" {
		t.Errorf("configured: Cfg.CacheDir=%q Cache.Root=%q err=%v", d.Cfg.CacheDir, d.Cache.Root, err)
	}
}
