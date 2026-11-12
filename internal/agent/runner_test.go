package agent

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/execx"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestArgs(t *testing.T) {
	s := Spec{Assignment: config.Assignment{Model: "opus", Effort: "high"}, System: "SYS", Schema: "SCH", AddDirs: []string{"/b"}}
	want := []string{"-p", "--model", "opus", "--effort", "high", "--output-format", "stream-json", "--verbose",
		"--json-schema", "SCH", "--restricted", "--strict-mcp-config", "--tools", "Read,Grep,Glob",
		"--setting-sources", "", "--permission-mode", "dontAsk", "--no-session-persistence",
		"--system-prompt", "SYS", "--add-dir", "/b"}
	if got := Args(s); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	s.Assignment.Effort = ""
	for _, a := range Args(s) {
		if a == "--effort" {
			t.Fatal("--effort must be omitted when empty")
		}
	}
}

func okRunner(stream string, fn func(execx.Cmd)) Runner {
	return Runner{Bin: "claude", Exec: execx.Fake(func(c execx.Cmd) (string, string, int) {
		if fn != nil {
			fn(c)
		}
		return stream, "", 0
	})}
}

func TestRunParsesStructuredOutput(t *testing.T) {
	r := okRunner(fixture(t, "stream_ok.jsonl"), nil)
	var acts []Activity
	out, err := r.Run(context.Background(), Spec{Name: "ag", Dir: "/w"}, func(a Activity) { acts = append(acts, a) })
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"hallazgos":[]}` {
		t.Fatalf("out = %s", out)
	}
	want := []Activity{{"ag", "leyendo src/a.go"}, {"ag", "buscando TODO"}}
	if !reflect.DeepEqual(acts, want) {
		t.Fatalf("acts = %v", acts)
	}
}

func TestRunPromptViaStdin(t *testing.T) {
	var gotIn, gotDir, gotName string
	r := okRunner(fixture(t, "stream_ok.jsonl"), func(c execx.Cmd) {
		b := new(strings.Builder)
		buf := make([]byte, 64)
		for {
			n, err := c.Stdin.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		gotIn, gotDir, gotName = b.String(), c.Dir, c.Name
	})
	if _, err := r.Run(context.Background(), Spec{Prompt: "revisá", Dir: "/w"}, nil); err != nil {
		t.Fatal(err)
	}
	if gotIn != "revisá" || gotDir != "/w" || gotName != "claude" {
		t.Fatalf("stdin=%q dir=%q name=%q", gotIn, gotDir, gotName)
	}
}

func TestRunErrors(t *testing.T) {
	var ae *AgentError
	_, err := okRunner(fixture(t, "stream_error.jsonl"), nil).Run(context.Background(), Spec{Name: "x"}, nil)
	if !errors.As(err, &ae) || ae.Agent != "x" || !strings.Contains(err.Error(), "se rompió") {
		t.Fatalf("is_error: %v", err)
	}
	_, err = okRunner(fixture(t, "stream_no_output.jsonl"), nil).Run(context.Background(), Spec{Name: "x"}, nil)
	if !errors.Is(err, ErrNoOutput) || !errors.As(err, &ae) {
		t.Fatalf("no output: %v", err)
	}
	_, err = okRunner("", nil).Run(context.Background(), Spec{Name: "x"}, nil)
	if !errors.Is(err, ErrNoOutput) {
		t.Fatalf("empty stream: %v", err)
	}
	bad := Runner{Bin: "claude", Exec: execx.Fake(func(execx.Cmd) (string, string, int) { return "", "boom", 2 })}
	_, err = bad.Run(context.Background(), Spec{Name: "x"}, nil)
	var ee *execx.ExitError
	if !errors.As(err, &ae) || !errors.As(err, &ee) || ee.Code != 2 {
		t.Fatalf("exit: %v", err)
	}
}

func TestRunTimeout(t *testing.T) {
	r := Runner{Bin: "claude", Timeout: 50 * time.Millisecond, Exec: func(ctx context.Context, c execx.Cmd) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	_, err := r.Run(context.Background(), Spec{Name: "x"}, nil)
	var ae *AgentError
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &ae) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunWritesLog(t *testing.T) {
	dir := t.TempDir()
	stream := fixture(t, "stream_ok.jsonl")
	r := okRunner(stream, nil)
	r.LogDir = filepath.Join(dir, "logs")
	if _, err := r.Run(context.Background(), Spec{Name: "ag", Dir: "/w"}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "logs", "ag.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != stream {
		t.Fatalf("log = %q", got)
	}
}

func TestSemaphoreLimitsConcurrency(t *testing.T) {
	var active, peak atomic.Int32
	r := Runner{Bin: "claude", Sem: make(chan struct{}, 2), Exec: execx.Fake(func(execx.Cmd) (string, string, int) {
		n := active.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return fixture(t, "stream_ok.jsonl"), "", 0
	})}
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.Run(context.Background(), Spec{Name: "x"}, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() != 2 {
		t.Fatalf("peak = %d, want 2", peak.Load())
	}
}

func TestSemaphoreRespectsContext(t *testing.T) {
	r := Runner{Sem: make(chan struct{}, 1), Exec: execx.Fake(func(execx.Cmd) (string, string, int) { return "", "", 0 })}
	r.Sem <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := r.Run(ctx, Spec{Name: "x"}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunOversizedLineKeepsResult(t *testing.T) {
	big := `{"type":"user","pad":"` + strings.Repeat("x", maxLine+1024) + `"}` + "\n"
	stream := big + fixture(t, "stream_ok.jsonl")
	dir := t.TempDir()
	r := okRunner(stream, nil)
	r.LogDir = dir
	out, err := r.Run(context.Background(), Spec{Name: "ag", Dir: "/w"}, nil)
	if err != nil || string(out) != `{"hallazgos":[]}` {
		t.Fatalf("out=%s err=%v", out, err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "ag.jsonl"))
	if !strings.Contains(string(got), `"type":"user"`) || !strings.HasSuffix(string(got), "\n") {
		t.Fatal("oversized line missing from log")
	}
}

func TestRunReadError(t *testing.T) {
	boom := errors.New("boom de lectura")
	r := Runner{Bin: "claude", Exec: func(ctx context.Context, c execx.Cmd) error {
		c.Stdout.Write([]byte("{\"type\":\"system\"}\n"))
		c.Stdout.(*io.PipeWriter).CloseWithError(boom)
		return nil
	}}
	_, err := r.Run(context.Background(), Spec{Name: "x"}, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunMalformedResultLine(t *testing.T) {
	_, err := okRunner("{\"type\":\"result\",\"is_error\":fal\n", nil).Run(context.Background(), Spec{Name: "x"}, nil)
	if !errors.Is(err, ErrNoOutput) || !strings.Contains(err.Error(), "inválid") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunExitKeepsStreamDiagnostic(t *testing.T) {
	r := Runner{Bin: "claude", Exec: execx.Fake(func(execx.Cmd) (string, string, int) {
		return fixture(t, "stream_error.jsonl"), "", 1
	})}
	_, err := r.Run(context.Background(), Spec{Name: "x"}, nil)
	var ee *execx.ExitError
	if !errors.As(err, &ee) || !strings.Contains(err.Error(), "se rompió") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunUnwritableLogDir(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, nil, 0o644)
	var called bool
	r := okRunner(fixture(t, "stream_ok.jsonl"), func(execx.Cmd) { called = true })
	r.LogDir = filepath.Join(blocker, "logs") // parent is a file: MkdirAll fails
	_, err := r.Run(context.Background(), Spec{Name: "x"}, nil)
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disco lleno") }

func TestLogWriteFailureDoesNotAbortButIsReportedOnFailure(t *testing.T) {
	res := parseStream(strings.NewReader(fixture(t, "stream_ok.jsonl")), "a", "/w", failWriter{}, nil)
	if string(res.output) != `{"hallazgos":[]}` || res.logErr == nil {
		t.Fatalf("res = %+v", res)
	}
}
