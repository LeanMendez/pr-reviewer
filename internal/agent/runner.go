// Package agent runs isolated `claude -p` reviewer agents.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/execx"
)

// ErrNoOutput means the agent finished without a structured result.
var ErrNoOutput = errors.New("el agente no devolvió resultado estructurado")

// Spec describes one agent invocation.
type Spec struct {
	Name       string
	Role       string
	Assignment config.Assignment
	System     string
	Prompt     string
	Schema     string
	Dir        string
	AddDirs    []string
}

// Activity is a live "what is the agent doing" line.
type Activity struct {
	Agent string
	Text  string
}

// Runner executes agents. A nil Sem imposes no concurrency limit; otherwise
// it must have capacity >= 1.
type Runner struct {
	Exec    execx.Exec
	Bin     string
	Timeout time.Duration
	Sem     chan struct{}
	LogDir  string
}

// AgentError attributes a failure to an agent.
type AgentError struct {
	Agent string
	Err   error
}

func (e *AgentError) Error() string { return fmt.Sprintf("agente %s: %v", e.Agent, e.Err) }
func (e *AgentError) Unwrap() error { return e.Err }

// Args builds the claude flags for a Spec. The prompt travels via stdin.
func Args(s Spec) []string {
	a := []string{"-p", "--model", string(s.Assignment.Model)}
	if s.Assignment.Effort != "" {
		a = append(a, "--effort", string(s.Assignment.Effort))
	}
	a = append(a,
		"--output-format", "stream-json", "--verbose",
		"--json-schema", s.Schema,
		"--restricted", "--strict-mcp-config",
		"--tools", "Read,Grep,Glob",
		"--setting-sources", "",
		"--permission-mode", "dontAsk",
		"--no-session-persistence",
		"--system-prompt", s.System,
	)
	for _, d := range s.AddDirs {
		a = append(a, "--add-dir", d)
	}
	return a
}

// Run executes the agent and returns its structured_output.
func (r Runner) Run(ctx context.Context, s Spec, onActivity func(Activity)) (json.RawMessage, error) {
	if r.Sem != nil {
		select {
		case r.Sem <- struct{}{}:
			defer func() { <-r.Sem }()
		case <-ctx.Done():
			return nil, &AgentError{s.Name, ctx.Err()}
		}
	}
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	var logf *os.File
	if r.LogDir != "" {
		if err := os.MkdirAll(r.LogDir, 0o755); err == nil {
			logf, _ = os.Create(filepath.Join(r.LogDir, filepath.Base(s.Name)+".jsonl"))
		}
		if logf != nil {
			defer logf.Close()
		}
	}

	pr, pw := io.Pipe()
	resCh := make(chan streamResult, 1)
	go func() {
		res := parseStream(pr, s.Name, s.Dir, logf, onActivity)
		io.Copy(io.Discard, pr) // keep draining so the process never blocks on a full pipe
		resCh <- res
	}()

	err := r.Exec(ctx, execx.Cmd{
		Name:   r.Bin,
		Args:   Args(s),
		Dir:    s.Dir,
		Stdin:  stringReader(s.Prompt),
		Stdout: pw,
	})
	pw.Close()
	res := <-resCh

	if err != nil {
		return nil, &AgentError{s.Name, err}
	}
	if res.isError {
		msg := res.message
		if msg == "" {
			msg = "el agente terminó con error"
		}
		return nil, &AgentError{s.Name, errors.New(msg)}
	}
	if len(res.output) == 0 {
		return nil, &AgentError{s.Name, ErrNoOutput}
	}
	return res.output, nil
}
