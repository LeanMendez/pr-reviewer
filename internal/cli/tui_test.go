package cli

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/LeanMendez/pr-review/internal/app"
	"github.com/LeanMendez/pr-review/internal/app/apptest"
	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/tui"
)

// gitOrigin answers `git remote get-url origin` with origin (no remote when
// empty) and records the dir it ran in.
func gitOrigin(origin string, dir *string) execx.Exec {
	return execx.Fake(func(c execx.Cmd) (string, string, int) {
		if c.Name == "git" && slices.Equal(c.Args, []string{"remote", "get-url", "origin"}) {
			*dir = c.Dir
			if origin == "" {
				return "", "error: No such remote 'origin'\n", 2
			}
			return origin + "\n", "", 0
		}
		return "", "", 1
	})
}

func TestDetectRepo(t *testing.T) {
	for url, want := range map[string]string{
		"git@bitbucket.org:ws/repo.git":           "ws/repo",
		"git@bitbucket.org:ws/repo":               "ws/repo",
		"https://bitbucket.org/ws/repo":           "ws/repo",
		"https://bitbucket.org/ws/repo.git":       "ws/repo",
		"https://leanm@bitbucket.org/ws/repo.git": "ws/repo",
		"ssh://git@bitbucket.org/ws/repo.git":     "ws/repo",
		"git@github.com:ws/repo.git":              "",
		"https://github.com/ws/repo.git":          "",
		"https://bitbucket.org.evil.com/ws/repo":  "",
		"https://bitbucket.org/ws":                "",
		"https://bitbucket.org/ws/repo/extra":     "",
		"git@bitbucket.org:/repo.git":             "",
		"https://bitbucket.org/ws/..":             "",
		"https://bitbucket.org/../repo":           "",
		"git@bitbucket.org:ws/...git":             "",
		"git@bitbucket.org:ws/.":                  "",
		"git@bitbucket.org:ws/re\x1bpo.git":       "",
		"git@bitbucket.org:w s/repo.git":          "",
		"git@bitbucket.org:ws/repo;rm.git":        "",
		"git@bitbucket.org:Equipo_1/api.v2-x.git": "Equipo_1/api.v2-x",
	} {
		var dir string
		got, ok := DetectRepo(gitOrigin(url, &dir), "/src/repo")
		if got != want || ok != (want != "") {
			t.Errorf("%s: got %q, %v; want %q", url, got, ok, want)
		}
		if dir != "/src/repo" {
			t.Errorf("%s: git ran in %q, want /src/repo", url, dir)
		}
	}
	var dir string
	if got, ok := DetectRepo(gitOrigin("", &dir), "/tmp"); ok || got != "" {
		t.Errorf("no remote: got %q, %v", got, ok)
	}
}

func TestRootArgs(t *testing.T) {
	t.Setenv("BITBUCKET_WORKSPACE", "")
	cwd, _ := os.Getwd()
	cases := []struct {
		name      string
		args      []string
		noConfig  bool
		workspace string
		env       string // BITBUCKET_WORKSPACE
		origin    string
		want      *tui.Start // nil: the interface must not open
		code      int
	}{
		{name: "repo", args: []string{"stock"}, workspace: "ws", want: &tui.Start{Repo: "ws/stock"}},
		{name: "workspace/repo", args: []string{"otro/stock"}, workspace: "ws", want: &tui.Start{Repo: "otro/stock"}},
		{name: "a single unknown word is a repo", args: []string{"revisar"}, workspace: "ws", want: &tui.Start{Repo: "ws/revisar"}},
		{name: "no config yet: the setup resolves it", args: []string{"stock"}, noConfig: true, want: &tui.Start{Repo: "stock"}},
		{name: "no config yet: the setup's workspace wins over the env", args: []string{"stock"}, noConfig: true, env: "del-entorno", want: &tui.Start{Repo: "stock"}},
		{name: "no config yet with workspace/repo", args: []string{"otro/stock"}, noConfig: true, env: "del-entorno", want: &tui.Start{Repo: "otro/stock"}},
		{name: "config without workspace", args: []string{"stock"}, code: 2},
		{name: "inside a Bitbucket checkout", workspace: "ws", origin: "git@bitbucket.org:equipo/api.git", want: &tui.Start{Repo: "equipo/api"}},
		{name: "outside one: the picker", workspace: "ws", want: &tui.Start{}},
		{name: "config models", args: []string{"config", "models"}, workspace: "ws", want: &tui.Start{Models: true}},
		{name: "two words", args: []string{"revisar", "stock"}, workspace: "ws", code: 2},
		{name: "typo'd subcommand with args", args: []string{"revisar", "stock", "477"}, workspace: "ws", code: 2},
		{name: "config models with args", args: []string{"config", "models", "x"}, workspace: "ws", code: 2},
		{name: "unknown config subcommand", args: []string{"config", "modelos"}, workspace: "ws", code: 2},
		{name: "review still works", args: []string{"review", "stock", "477", "--mode", "ligero"}, workspace: "ws"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("BITBUCKET_WORKSPACE", c.env)
			h := apptest.New(t)
			h.Deps.ConfigFound = !c.noConfig
			h.Deps.Cfg.Workspace = c.workspace
			var dir string
			origin := gitOrigin(c.origin, &dir)
			inner := h.Deps.Exec
			h.Deps.Exec = func(ctx context.Context, cmd execx.Cmd) error {
				if cmd.Name == "git" && len(cmd.Args) > 0 && cmd.Args[0] == "remote" {
					return origin(ctx, cmd)
				}
				return inner(ctx, cmd)
			}
			var got *tui.Start
			ui := func(_ context.Context, d app.Deps, s tui.Start) error {
				if d.Cfg.Workspace != c.workspace {
					t.Errorf("the interface got workspace %q", d.Cfg.Workspace)
				}
				got = &s
				return nil
			}
			root := newRootCmd("test", func() (app.Deps, error) { return h.Deps, nil }, ui)
			root.SetOut(&h.Out)
			root.SetErr(&h.Err)
			root.SetArgs(c.args)
			err := root.ExecuteContext(context.Background())
			if code := exitCodeOf(err); code != c.code {
				t.Fatalf("code = %d (%v), want %d", code, err, c.code)
			}
			switch {
			case c.want == nil && got != nil:
				t.Errorf("the interface opened with %+v", *got)
			case c.want != nil && got == nil:
				t.Errorf("the interface did not open, want %+v", *c.want)
			case c.want != nil && *got != *c.want:
				t.Errorf("opened with %+v, want %+v", *got, *c.want)
			}
			if c.origin != "" && dir != cwd {
				t.Errorf("detected the repo in %q, want the working dir %q", dir, cwd)
			}
		})
	}
}

func TestRootUIErrorIsReturned(t *testing.T) {
	h := apptest.New(t)
	h.Deps.ConfigFound = true
	boom := errors.New("sin terminal")
	root := newRootCmd("test", func() (app.Deps, error) { return h.Deps, nil },
		func(context.Context, app.Deps, tui.Start) error { return boom })
	root.SetArgs([]string{"stock"})
	if err := root.ExecuteContext(context.Background()); !errors.Is(err, boom) || exitCodeOf(err) != 1 {
		t.Errorf("err = %v, want the interface's error with exit 1", err)
	}
}
