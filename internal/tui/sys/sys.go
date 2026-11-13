// Package sys wraps the desktop actions the TUI needs: clipboard, browser
// and notifications. Everything runs through an injected Exec.
package sys

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/LeanMendez/pr-review/internal/execx"
)

// ErrNoClipboard means none of wl-copy, xclip or xsel is installed.
var ErrNoClipboard = errors.New("no clipboard tool found")

// System runs desktop actions. LookPath says whether a tool is installed.
type System struct {
	Exec     execx.Exec
	LookPath func(string) (string, error)
}

// timeout bounds every system action.
const timeout = 5 * time.Second

var clipboardTools = []struct {
	name string
	args []string
}{
	{"wl-copy", nil},
	{"xclip", []string{"-selection", "clipboard"}},
	{"xsel", []string{"--clipboard", "--input"}},
}

// Copy puts text on the clipboard with the first installed tool that works.
// An installed tool can still fail for the session (wl-copy on X11 cannot
// reach a Wayland server), so a failure moves on to the next one; when every
// installed tool fails, their errors come back joined.
func (s System) Copy(ctx context.Context, text string) error {
	var errs []error
	for _, t := range clipboardTools {
		if _, err := s.LookPath(t.name); err != nil {
			continue
		}
		if err := s.copyWith(ctx, t.name, t.args, text); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.name, err))
			continue
		}
		return nil
	}
	if len(errs) == 0 {
		return ErrNoClipboard
	}
	return errors.Join(errs...)
}

func (s System) copyWith(ctx context.Context, name string, args []string, text string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s.Exec(ctx, execx.Cmd{Name: name, Args: args, Stdin: strings.NewReader(text), Detach: true})
}

// Open opens an http or https URL in the default browser; anything else is
// refused without running a command.
func (s System) Open(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("not an http(s) URL: %q", rawURL)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s.Exec(ctx, execx.Cmd{Name: "xdg-open", Args: []string{rawURL}, Detach: true})
}

// markup escapes what notify-send would read as markup: PR titles and model
// text reach the notification.
var markup = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// Notify shows a desktop notification. Without notify-send it does nothing.
// Title and body are plain text: markup characters are escaped.
func (s System) Notify(ctx context.Context, title, body string) error {
	if _, err := s.LookPath("notify-send"); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := []string{"--", markup.Replace(title), markup.Replace(body)}
	return s.Exec(ctx, execx.Cmd{Name: "notify-send", Args: args, Detach: true})
}
