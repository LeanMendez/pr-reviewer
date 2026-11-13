// Package sys wraps the desktop actions the TUI needs: clipboard, browser
// and notifications. Everything runs through an injected Exec.
package sys

import (
	"context"
	"errors"
	"strings"

	"github.com/LeanMendez/pr-review/internal/execx"
)

// ErrNoClipboard means none of wl-copy, xclip or xsel is installed.
var ErrNoClipboard = errors.New("no clipboard tool found")

// System runs desktop actions. LookPath says whether a tool is installed.
type System struct {
	Exec     execx.Exec
	LookPath func(string) (string, error)
}

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
		return s.Exec(ctx, execx.Cmd{Name: t.name, Args: t.args, Stdin: strings.NewReader(text)})
	}
	return ErrNoClipboard
}

// Open opens url in the default browser.
func (s System) Open(ctx context.Context, url string) error {
	return s.Exec(ctx, execx.Cmd{Name: "xdg-open", Args: []string{url}})
}

// Notify shows a desktop notification. Without notify-send it does nothing.
func (s System) Notify(ctx context.Context, title, body string) error {
	if _, err := s.LookPath("notify-send"); err != nil {
		return nil
	}
	return s.Exec(ctx, execx.Cmd{Name: "notify-send", Args: []string{"--", title, body}})
}
