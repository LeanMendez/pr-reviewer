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

// Copy puts text on the clipboard using the first tool that is installed.
func (s System) Copy(ctx context.Context, text string) error {
	for _, t := range clipboardTools {
		if _, err := s.LookPath(t.name); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return s.Exec(ctx, execx.Cmd{Name: t.name, Args: t.args, Stdin: strings.NewReader(text), Detach: true})
	}
	return ErrNoClipboard
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
