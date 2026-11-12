// Package prb wraps the prismatic-rainbow CLI (`prb`) and its JSON output.
package prb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/LeanMendez/pr-review/internal/execx"
)

// Client runs prb through an injectable Exec.
type Client struct {
	Exec execx.Exec
	Bin  string // defaults to "prb"
}

// Error is a structured prb failure parsed from its stderr JSON.
type Error struct {
	Code           string
	Message        string
	Retryable      bool
	RequiredScopes []string
}

func (e *Error) Error() string {
	return fmt.Sprintf("prb: %s: %s", e.Code, e.Message)
}

func (c Client) bin() string {
	if c.Bin == "" {
		return "prb"
	}
	return c.Bin
}

// run executes prb and returns stdout. Non-zero exits become *Error.
func (c Client) run(ctx context.Context, args ...string) ([]byte, error) {
	var out bytes.Buffer
	err := c.Exec(ctx, execx.Cmd{Name: c.bin(), Args: args, Stdout: &out})
	if err == nil {
		return out.Bytes(), nil
	}
	var ee *execx.ExitError
	if errors.As(err, &ee) {
		return nil, parseError(ee)
	}
	return nil, err
}

func parseError(ee *execx.ExitError) error {
	var payload struct {
		Error struct {
			Code           string   `json:"code"`
			Message        string   `json:"message"`
			RequiredScopes []string `json:"required_scopes"`
			Retryable      bool     `json:"retryable"`
		} `json:"error"`
	}
	for _, line := range strings.Split(ee.Stderr, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		if json.Unmarshal([]byte(line), &payload) == nil && payload.Error.Code != "" {
			e := payload.Error
			return &Error{Code: e.Code, Message: e.Message, Retryable: e.Retryable, RequiredScopes: e.RequiredScopes}
		}
	}
	if (json.Unmarshal([]byte(strings.TrimSpace(ee.Stderr)), &payload) == nil) && payload.Error.Code != "" {
		e := payload.Error
		return &Error{Code: e.Code, Message: e.Message, Retryable: e.Retryable, RequiredScopes: e.RequiredScopes}
	}
	msg := strings.TrimSpace(ee.Stderr)
	if msg == "" {
		msg = fmt.Sprintf("exit status %d", ee.Code)
	}
	code := "exit_" + strconv.Itoa(ee.Code)
	if ee.Code == 4 {
		code = "no_credentials"
	}
	return &Error{Code: code, Message: msg}
}

func decode[T any](data []byte, what string) (T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("prb %s: invalid JSON: %w", what, err)
	}
	return v, nil
}

// AuthStatus runs `prb auth status -o json`.
func (c Client) AuthStatus(ctx context.Context) (Identity, error) {
	out, err := c.run(ctx, "auth", "status", "-o", "json")
	if err != nil {
		return Identity{}, err
	}
	return decode[Identity](out, "auth status")
}

// ListOpen lists the open PRs of repo.
func (c Client) ListOpen(ctx context.Context, repo string) ([]PRSummary, error) {
	out, err := c.run(ctx, "pr", "list", "-R", repo, "--state", "OPEN", "--limit", "0", "-o", "json")
	if err != nil {
		return nil, err
	}
	return decode[[]PRSummary](out, "pr list")
}

// Context returns the parsed PR bundle plus the raw JSON.
func (c Client) Context(ctx context.Context, repo string, n int) (PRContext, []byte, error) {
	out, err := c.run(ctx, "pr", "context", strconv.Itoa(n), "-R", repo, "-o", "json")
	if err != nil {
		return PRContext{}, nil, err
	}
	pc, err := decode[PRContext](out, "pr context")
	if err != nil {
		return PRContext{}, nil, err
	}
	return pc, out, nil
}

// Diff returns the PR diff, skipping files that match any exclude glob.
func (c Client) Diff(ctx context.Context, repo string, n int, exclude []string) (Diff, error) {
	args := []string{"pr", "diff", strconv.Itoa(n), "-R", repo, "-o", "json", "--max-bytes", "2000000"}
	for _, g := range exclude {
		args = append(args, "--exclude", g)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return Diff{}, err
	}
	return decode[Diff](out, "pr diff")
}

// Repos lists every repository of a workspace.
func (c Client) Repos(ctx context.Context, workspace string) ([]Repo, error) {
	out, err := c.run(ctx, "repo", "list", "-w", workspace, "--limit", "0", "-o", "json")
	if err != nil {
		return nil, err
	}
	return decode[[]Repo](out, "repo list")
}
