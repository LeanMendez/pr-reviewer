package execx

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRealRunsCommand(t *testing.T) {
	var out bytes.Buffer
	err := Real()(context.Background(), Cmd{Name: "go", Args: []string{"version"}, Stdout: &out})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "go version") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestRealStdin(t *testing.T) {
	var out bytes.Buffer
	err := Real()(context.Background(), Cmd{Name: "cat", Stdin: strings.NewReader("hola"), Stdout: &out})
	if err != nil || out.String() != "hola" {
		t.Fatalf("err=%v out=%q", err, out.String())
	}
}

func TestRealExitError(t *testing.T) {
	var tee bytes.Buffer
	err := Real()(context.Background(), Cmd{Name: "go", Args: []string{"definitely-invalid-subcommand"}, Stderr: &tee})
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("want *ExitError, got %T: %v", err, err)
	}
	if ee.Code == 0 || ee.Stderr == "" {
		t.Fatalf("ExitError = %+v", ee)
	}
	if tee.String() != ee.Stderr {
		t.Fatalf("caller stderr %q != captured %q", tee.String(), ee.Stderr)
	}
}

func TestRealContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := Real()(ctx, Cmd{Name: "sleep", Args: []string{"5"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
}

func TestRealMissingBinary(t *testing.T) {
	err := Real()(context.Background(), Cmd{Name: "no-such-binary-xyz"})
	if err == nil {
		t.Fatal("expected error")
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		t.Fatalf("start failure must not be ExitError: %v", err)
	}
}

func TestFakeWritesStdout(t *testing.T) {
	var out, errb bytes.Buffer
	f := Fake(func(c Cmd) (string, string, int) {
		if c.Name != "prb" {
			t.Errorf("name = %q", c.Name)
		}
		return "salida", "aviso", 0
	})
	if err := f(context.Background(), Cmd{Name: "prb", Stdout: &out, Stderr: &errb}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "salida" || errb.String() != "aviso" {
		t.Fatalf("out=%q err=%q", out.String(), errb.String())
	}
}

func TestFakeNonZeroAndNilWriters(t *testing.T) {
	f := Fake(func(Cmd) (string, string, int) { return "x", "boom", 3 })
	err := f(context.Background(), Cmd{Name: "prb"})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 3 || ee.Stderr != "boom" {
		t.Fatalf("err = %v", err)
	}
}
