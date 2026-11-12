package repocache

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/prb"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// fixture builds a bare origin with branches develop and feature (one extra
// commit adding feature.txt) and returns the origin path and both commits.
func fixture(t *testing.T) (origin, develop, feature string) {
	t.Helper()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "work")
	origin = filepath.Join(tmp, "origin.git")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, work, "init", "-q", "-b", "develop")
	if err := os.WriteFile(filepath.Join(work, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "base")
	develop = git(t, work, "rev-parse", "HEAD")
	git(t, work, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "feature")
	feature = git(t, work, "rev-parse", "HEAD")
	git(t, tmp, "clone", "-q", "--bare", work, origin)
	return origin, develop, feature
}

func newPR(develop, feature string) prb.PR {
	return prb.PR{
		ID:          7,
		Source:      prb.Ref{Branch: "feature", Commit: feature[:12], Repository: "ws/repo"},
		Destination: prb.Ref{Branch: "develop", Commit: develop[:12], Repository: "ws/repo"},
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestPrepareCreatesWorktrees(t *testing.T) {
	origin, develop, feature := fixture(t)
	c := Cache{Exec: execx.Real(), Root: t.TempDir(), CloneURL: origin}
	wt, cleanup, err := c.Prepare(context.Background(), "ws", "repo", newPR(develop, feature))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !exists(filepath.Join(wt.Head, "feature.txt")) {
		t.Error("head should contain feature.txt")
	}
	if exists(filepath.Join(wt.Base, "feature.txt")) {
		t.Error("base should not contain feature.txt")
	}
	if !exists(filepath.Join(wt.Base, "base.txt")) {
		t.Error("base should contain base.txt")
	}
	if got := git(t, wt.Head, "rev-parse", "HEAD"); got != feature {
		t.Errorf("head at %s, want %s", got, feature)
	}
	if got := git(t, wt.Base, "rev-parse", "HEAD"); got != develop {
		t.Errorf("base at %s, want %s", got, develop)
	}
}

func TestPrepareReusesClone(t *testing.T) {
	origin, develop, feature := fixture(t)
	c := Cache{Exec: execx.Real(), Root: t.TempDir(), CloneURL: origin}
	pr := newPR(develop, feature)
	_, cleanup, err := c.Prepare(context.Background(), "ws", "repo", pr)
	if err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(c.Root, "ws", "repo", ".git")
	before, err := os.Stat(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(gitDir, "marker")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Re-preparing the same PR without cleanup must also work (stale dirs).
	_, cleanup2, err := c.Prepare(context.Background(), "ws", "repo", pr)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !exists(marker) {
		t.Error("clone was recreated")
	}
	_ = cleanup()
	_ = cleanup2()
}

func TestCleanupRemovesWorktrees(t *testing.T) {
	origin, develop, feature := fixture(t)
	c := Cache{Exec: execx.Real(), Root: t.TempDir(), CloneURL: origin}
	wt, cleanup, err := c.Prepare(context.Background(), "ws", "repo", newPR(develop, feature))
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if exists(wt.Head) || exists(wt.Base) {
		t.Error("worktrees still exist")
	}
	out := git(t, filepath.Join(c.Root, "ws", "repo"), "worktree", "list")
	if strings.Contains(out, "pr7-") {
		t.Errorf("worktrees still registered:\n%s", out)
	}
}

func TestForkUnsupported(t *testing.T) {
	root := filepath.Join(t.TempDir(), "never")
	var buf bytes.Buffer
	c := Cache{
		Exec: execx.Fake(func(cmd execx.Cmd) (string, string, int) {
			buf.WriteString("called")
			return "", "", 0
		}),
		Root: root, CloneURL: "x",
	}
	pr := prb.PR{ID: 1,
		Source:      prb.Ref{Repository: "someone/fork"},
		Destination: prb.Ref{Repository: "ws/repo"}}
	_, _, err := c.Prepare(context.Background(), "ws", "repo", pr)
	if !errors.Is(err, ErrForkUnsupported) {
		t.Fatalf("err = %v", err)
	}
	if buf.Len() != 0 || exists(root) {
		t.Error("touched exec or disk")
	}
}
