package sys

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"slices"
	"testing"

	"github.com/LeanMendez/pr-review/internal/execx"
)

type call struct {
	name  string
	args  []string
	stdin string
}

func fakeSystem(available ...string) (System, *[]call) {
	var calls []call
	s := System{
		Exec: func(_ context.Context, c execx.Cmd) error {
			cl := call{name: c.Name, args: c.Args}
			if c.Stdin != nil {
				b, _ := io.ReadAll(c.Stdin)
				cl.stdin = string(b)
			}
			calls = append(calls, cl)
			return nil
		},
		LookPath: func(name string) (string, error) {
			if slices.Contains(available, name) {
				return "/usr/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		},
	}
	return s, &calls
}

func TestSysCopyPicksFirstAvailable(t *testing.T) {
	cases := []struct {
		have []string
		want call
	}{
		{[]string{"wl-copy", "xclip", "xsel"}, call{"wl-copy", nil, "hola"}},
		{[]string{"xclip", "xsel"}, call{"xclip", []string{"-selection", "clipboard"}, "hola"}},
		{[]string{"xsel"}, call{"xsel", []string{"--clipboard", "--input"}, "hola"}},
	}
	for _, c := range cases {
		s, calls := fakeSystem(c.have...)
		if err := s.Copy(context.Background(), "hola"); err != nil {
			t.Fatalf("Copy with %v: %v", c.have, err)
		}
		if len(*calls) != 1 || (*calls)[0].name != c.want.name || !slices.Equal((*calls)[0].args, c.want.args) || (*calls)[0].stdin != "hola" {
			t.Errorf("Copy with %v ran %+v, want %+v", c.have, *calls, c.want)
		}
	}
}

func TestSysCopyNoClipboard(t *testing.T) {
	s, calls := fakeSystem()
	if err := s.Copy(context.Background(), "x"); !errors.Is(err, ErrNoClipboard) {
		t.Errorf("err = %v, want ErrNoClipboard", err)
	}
	if len(*calls) != 0 {
		t.Errorf("nothing should run, got %+v", *calls)
	}
}

func TestSysCopyReturnsExecError(t *testing.T) {
	s, _ := fakeSystem("wl-copy")
	boom := errors.New("boom")
	s.Exec = func(context.Context, execx.Cmd) error { return boom }
	if err := s.Copy(context.Background(), "x"); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}

func TestSysOpen(t *testing.T) {
	s, calls := fakeSystem("xdg-open")
	if err := s.Open(context.Background(), "https://bitbucket.org/x"); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0]; got.name != "xdg-open" || !slices.Equal(got.args, []string{"https://bitbucket.org/x"}) {
		t.Errorf("Open ran %+v", got)
	}
}

func TestSysNotify(t *testing.T) {
	s, calls := fakeSystem("notify-send")
	if err := s.Notify(context.Background(), "Listo", "PR #1"); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0]; got.name != "notify-send" || !slices.Equal(got.args, []string{"--", "Listo", "PR #1"}) {
		t.Errorf("Notify ran %+v", got)
	}
}

func TestSysNotifyWithoutNotifySend(t *testing.T) {
	s, calls := fakeSystem()
	if err := s.Notify(context.Background(), "a", "b"); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if len(*calls) != 0 {
		t.Errorf("nothing should run, got %+v", *calls)
	}
}
