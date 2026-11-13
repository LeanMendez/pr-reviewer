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
	name             string
	args             []string
	stdin            string
	detach, deadline bool
}

func fakeSystem(available ...string) (System, *[]call) {
	var calls []call
	s := System{
		Exec: func(ctx context.Context, c execx.Cmd) error {
			_, dl := ctx.Deadline()
			cl := call{name: c.Name, args: c.Args, detach: c.Detach, deadline: dl}
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
		{[]string{"wl-copy", "xclip", "xsel"}, call{name: "wl-copy", args: nil, stdin: "hola"}},
		{[]string{"xclip", "xsel"}, call{name: "xclip", args: []string{"-selection", "clipboard"}, stdin: "hola"}},
		{[]string{"xsel"}, call{name: "xsel", args: []string{"--clipboard", "--input"}, stdin: "hola"}},
	}
	for _, c := range cases {
		s, calls := fakeSystem(c.have...)
		if err := s.Copy(context.Background(), "hola"); err != nil {
			t.Fatalf("Copy with %v: %v", c.have, err)
		}
		if len(*calls) != 1 || (*calls)[0].name != c.want.name || !slices.Equal((*calls)[0].args, c.want.args) || (*calls)[0].stdin != "hola" || !(*calls)[0].detach || !(*calls)[0].deadline {
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
	if got := (*calls)[0]; got.name != "xdg-open" || !got.detach || !got.deadline || !slices.Equal(got.args, []string{"https://bitbucket.org/x"}) {
		t.Errorf("Open ran %+v", got)
	}
}

func TestSysNotify(t *testing.T) {
	s, calls := fakeSystem("notify-send")
	if err := s.Notify(context.Background(), "Listo", "PR #1"); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0]; got.name != "notify-send" || !got.deadline || !slices.Equal(got.args, []string{"--", "Listo", "PR #1"}) {
		t.Errorf("Notify ran %+v", got)
	}
}

func TestSysNotifyEscapesMarkup(t *testing.T) {
	s, calls := fakeSystem("notify-send")
	if err := s.Notify(context.Background(), "a<b>", `#7 listo: <a href="x">R&D</a>`); err != nil {
		t.Fatal(err)
	}
	want := []string{"--", "a&lt;b&gt;", `#7 listo: &lt;a href="x"&gt;R&amp;D&lt;/a&gt;`}
	if got := (*calls)[0].args; !slices.Equal(got, want) {
		t.Errorf("Notify args = %q, want %q", got, want)
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

func TestSysOpenOnlyHTTP(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "-o/tmp/x", "--help", "", "ftp://x/y", "https:///nohost", "bitbucket.org/x"} {
		s, calls := fakeSystem("xdg-open")
		if err := s.Open(context.Background(), u); err == nil {
			t.Errorf("Open(%q) succeeded", u)
		}
		if len(*calls) != 0 {
			t.Errorf("Open(%q) ran %+v", u, *calls)
		}
	}
	s, calls := fakeSystem("xdg-open")
	if err := s.Open(context.Background(), "http://example.com/a?b=1"); err != nil || len(*calls) != 1 {
		t.Errorf("http URL rejected: %v %+v", err, *calls)
	}
}
