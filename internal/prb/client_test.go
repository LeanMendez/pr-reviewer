package prb

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/LeanMendez/pr-review/internal/execx"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fake returns a Client whose Exec records the args and answers with stdout.
func fake(stdout, stderr string, code int, got *[]string) Client {
	return Client{Exec: execx.Fake(func(c execx.Cmd) (string, string, int) {
		if got != nil {
			*got = c.Args
		}
		return stdout, stderr, code
	})}
}

func TestContextParses(t *testing.T) {
	raw := fixture(t, "context_ok.json")
	pc, gotRaw, err := fake(raw, "", 0, nil).Context(context.Background(), "ws/stock", 42)
	if err != nil {
		t.Fatal(err)
	}
	if pc.PullRequest.ID != 42 || !pc.PullRequest.Draft {
		t.Errorf("pr = %+v", pc.PullRequest)
	}
	if pc.Checks == nil || pc.Checks.Overall != "pass" {
		t.Errorf("checks = %+v", pc.Checks)
	}
	if pc.Tasks == nil || pc.Tasks.Unresolved != 1 {
		t.Errorf("tasks = %+v", pc.Tasks)
	}
	if got := pc.PullRequest.Review.Reviewers[0].State; got != "changes_requested" {
		t.Errorf("reviewer state = %q", got)
	}
	if pc.Files == nil || len(pc.Files.Files) != 2 || pc.Files.Files[1].OldPath != "internal/router.go" {
		t.Errorf("files = %+v", pc.Files)
	}
	if len(pc.TaskReferences) != 1 || pc.TaskReferences[0].ID != "SAL-17" {
		t.Errorf("task refs = %+v", pc.TaskReferences)
	}
	if string(gotRaw) != raw {
		t.Error("raw JSON differs from fixture")
	}
}

func TestContextBuildsArgs(t *testing.T) {
	var got []string
	_, _, err := fake(fixture(t, "context_ok.json"), "", 0, &got).Context(context.Background(), "ws/stock", 42)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pr", "context", "42", "-R", "ws/stock", "-o", "json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestDiffExcludeArgs(t *testing.T) {
	var got []string
	d, err := fake(fixture(t, "diff.json"), "", 0, &got).Diff(context.Background(), "ws/stock", 42, []string{"*.lock", "vendor/**"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pr", "diff", "42", "-R", "ws/stock", "-o", "json", "--max-bytes", "2000000",
		"--exclude", "*.lock", "--exclude", "vendor/**"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
	if d.TotalFiles != 3 || !d.Truncated || len(d.Files) != 3 || !d.Files[1].Binary || !d.Files[2].PatchOmitted || d.Files[2].OmittedLines != 850 {
		t.Errorf("diff = %+v", d)
	}
}

func TestErrorParsing(t *testing.T) {
	indented := fixture(t, "error_missing_scopes.json")
	oneline := fixture(t, "error_missing_scopes_oneline.json")
	cases := map[string]string{
		"indented":         indented,
		"single line":      oneline,
		"text before JSON": "warning: {retrying} request\n" + indented,
		"text around JSON": "prb: failed\n" + indented + "hint: run prb auth login\n",
	}
	for name, stderr := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := fake("", stderr, 1, nil).Context(context.Background(), "ws/stock", 42)
			var pe *Error
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want *Error", err)
			}
			if pe.Code != "missing_scopes" || len(pe.RequiredScopes) != 1 || pe.Retryable {
				t.Errorf("error = %+v", pe)
			}
		})
	}
}

func TestExit4WithoutJSONIsNoCredentials(t *testing.T) {
	_, err := fake("", "no credentials configured\n", 4, nil).AuthStatus(context.Background())
	var pe *Error
	if !errors.As(err, &pe) || pe.Code != "no_credentials" {
		t.Fatalf("err = %v", err)
	}
}

func TestChecksNull(t *testing.T) {
	pc, _, err := fake(fixture(t, "context_checks_null.json"), "", 0, nil).Context(context.Background(), "ws/stock", 43)
	if err != nil {
		t.Fatal(err)
	}
	if pc.Checks != nil || len(pc.Warnings) == 0 || pc.Warnings[0].Section != "checks" {
		t.Errorf("checks = %+v warnings = %+v", pc.Checks, pc.Warnings)
	}
}

func TestAuthStatus(t *testing.T) {
	var got []string
	id, err := fake(fixture(t, "auth_status.json"), "", 0, &got).AuthStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"auth", "status", "-o", "json"}) {
		t.Errorf("args = %v", got)
	}
	if !id.Authenticated || id.User != "Ana Perez" || id.AccountID != "557058:aaaa-bbbb" || id.DefaultWorkspace != "ws" {
		t.Errorf("identity = %+v", id)
	}
}

func TestListOpen(t *testing.T) {
	var got []string
	prs, err := fake(fixture(t, "list.json"), "", 0, &got).ListOpen(context.Background(), "ws/stock")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pr", "list", "-R", "ws/stock", "--state", "OPEN", "--limit", "0", "-o", "json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v", got)
	}
	if len(prs) != 2 || prs[0].ID != 42 || !prs[0].Draft || prs[1].Author != "Bruno Gomez" {
		t.Errorf("prs = %+v", prs)
	}
}

func TestRepos(t *testing.T) {
	var got []string
	repos, err := fake(fixture(t, "repos.json"), "", 0, &got).Repos(context.Background(), "ws")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"repo", "list", "-w", "ws", "--limit", "0", "-o", "json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v", got)
	}
	if len(repos) != 2 || repos[0].Slug != "stock" || repos[0].FullName != "ws/stock" {
		t.Errorf("repos = %+v", repos)
	}
}

func TestDefaultBin(t *testing.T) {
	var name string
	c := Client{Exec: execx.Fake(func(cmd execx.Cmd) (string, string, int) { name = cmd.Name; return "[]", "", 0 })}
	if _, err := c.Repos(context.Background(), "ws"); err != nil || name != "prb" {
		t.Fatalf("err=%v name=%q", err, name)
	}
}
