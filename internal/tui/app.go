// Package tui is the terminal interface of pr-review: a router over the
// screens that runs reviews in the background.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LeanMendez/pr-review/internal/app"
	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/report"
	"github.com/LeanMendez/pr-review/internal/tui/listdata"
	"github.com/LeanMendez/pr-review/internal/tui/screens"
	"github.com/LeanMendez/pr-review/internal/tui/sys"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// Start says where the interface opens.
type Start struct {
	// Repo is "workspace/repo", or a bare name when there is no config yet
	// (the first-run setup resolves it with the workspace it asks for).
	// Empty opens the repo picker.
	Repo string
	// Models opens only the models screen: pr-review config models.
	Models bool
}

type screen int

const (
	scrOnboarding screen = iota
	scrPicker
	scrList
	scrMode
	scrProgress
	scrReport
	scrModels
)

// session is what the router and its reviews share. It changes when the
// user finishes the setup, saves the models or picks a repo.
type session struct {
	d    app.Deps
	repo string // "workspace/repo"
	root string // where reports are, as app.Review saves them
}

func (s *session) setConfig(cfg config.Config) {
	old := s.d.Cfg.ReportsDir
	s.d.Cfg, s.d.ConfigFound = cfg, true
	if cfg.ReportsDir != old {
		s.root = app.ReportsRoot(s.d)
	}
}

// App is the router: it shows one screen at a time, connects the screens
// with prb, the config and the reviews, and keeps the reviews running while
// the user is elsewhere.
type App struct {
	s       *session
	sys     sys.System
	ctx     context.Context
	reviews *Reviews
	save    func(config.Config) error
	now     func() time.Time

	modelsOnly bool
	screen     screen
	onboarding screens.OnboardingModel
	picker     screens.RepoPickerModel
	list       screens.ListModel
	mode       screens.ModeModel
	report     screens.ReportModel
	models     screens.ModelsModel
	progressPR int // the review whose progress is shown

	confirming bool // asking whether to quit with reviews running
	quitting   bool // waiting for the cancelled reviews to end
	forced     bool // quit with ctrl+c without waiting for the cleanup

	width, height int
}

// NewApp builds the interface for repo ("workspace/repo"; empty opens the
// repo picker). Without a config file it starts with the first-run setup.
func NewApp(d app.Deps, s sys.System, repo string) App {
	return newApp(context.Background(), d, s, Start{Repo: repo})
}

// NewModelsApp builds the interface of pr-review config models: the models
// screen alone (the first-run setup when there is no config yet), which
// quits when the user saves or leaves.
func NewModelsApp(d app.Deps, s sys.System) App {
	return newApp(context.Background(), d, s, Start{Models: true})
}

func newApp(ctx context.Context, d app.Deps, s sys.System, start Start) App {
	sess := &session{d: d, repo: start.Repo, root: app.ReportsRoot(d)}
	a := App{s: sess, sys: s, ctx: ctx, save: saveConfig, now: time.Now, modelsOnly: start.Models}
	a.reviews = newReviews(ctx, func(ctx context.Context, row listdata.Row, mode pipeline.Mode, events chan<- pipeline.Event) tea.Cmd {
		return ReviewCmd(ctx, sess.d, sess.repo, row, mode, events)
	})
	switch {
	case !d.ConfigFound:
		a, _ = a.openOnboarding()
	case start.Models:
		a, _ = a.openModels()
	case start.Repo == "":
		a, _ = a.openPicker()
	default:
		a, _ = a.openList(start.Repo)
	}
	return a
}

// Run opens the interface in the alternate screen and returns when the user
// quits. Reviews still running then (a signal ended the program, or the
// user forced the quit) are cancelled.
func Run(ctx context.Context, d app.Deps, start Start) error {
	a := newApp(ctx, d, sys.System{Exec: d.Exec, LookPath: exec.LookPath}, start)
	final, err := tea.NewProgram(a).Run()
	shutdown(final, a.reviews)
	return err
}

// shutdown cancels the reviews still running and waits until they remove
// their worktrees, unless the user forced the quit with a second ctrl+c.
func shutdown(final tea.Model, r *Reviews) {
	r.CancelAll()
	if a, ok := final.(App); ok && a.forced {
		return
	}
	r.Wait()
}

// Init starts what the first screen loads.
func (a App) Init() tea.Cmd {
	switch a.screen {
	case scrOnboarding:
		return a.onboarding.Init()
	case scrPicker:
		return a.picker.Init()
	case scrList:
		return a.list.Init()
	}
	return nil
}

// View renders the active screen; while quitting, its last line (the key
// help) becomes the question or the wait.
func (a App) View() tea.View {
	v := a.active().View()
	const force = "  ctrl+c sale ya, sin esperar la limpieza"
	n := a.reviews.Running()
	switch {
	case a.confirming:
		v.Content = withStatus(v.Content, a.width, fmt.Sprintf("Hay %s en curso. ¿Salir igual? (s/n)", reviewsText(n))+force)
	case a.quitting:
		v.Content = withStatus(v.Content, a.width, fmt.Sprintf("Cancelando %s en curso…", reviewsText(n))+force)
	}
	v.AltScreen = true
	return v
}

func reviewsText(n int) string {
	if n == 1 {
		return "1 revisión"
	}
	return fmt.Sprintf("%d revisiones", n)
}

func withStatus(content string, width int, text string) string {
	lines := strings.Split(content, "\n")
	line := " " + theme.Accent.Render(text)
	if width > 0 {
		line = ansi.Truncate(line, width, "…")
	}
	lines[len(lines)-1] = line
	return strings.Join(lines, "\n")
}

func (a App) active() tea.Model {
	switch a.screen {
	case scrOnboarding:
		return a.onboarding
	case scrPicker:
		return a.picker
	case scrMode:
		return a.mode
	case scrProgress:
		if rv := a.reviews.get(a.progressPR); rv != nil {
			return rv.progress
		}
	case scrReport:
		return a.report
	case scrModels:
		return a.models
	}
	return a.list
}

// loadRows builds the list's load: who the user is (to hide their own PRs)
// and the open PRs of repo, with their last review under the reports root.
func (a App) loadRows(repo string) func() tea.Cmd {
	ctx, prbc, cfg, root := a.ctx, a.s.d.PRB, a.s.d.Cfg, a.s.root
	return func() tea.Cmd {
		return func() tea.Msg {
			me, err := prbc.AuthStatus(ctx)
			if err != nil {
				return screens.RowsLoadedMsg{Err: err}
			}
			ready, hidden, err := listdata.Loader{PRB: prbc, Me: me, ReportsRoot: root, Cfg: cfg}.Load(ctx, repo)
			return screens.RowsLoadedMsg{Ready: ready, Hidden: hidden, Err: err}
		}
	}
}

// loadRepos builds the picker's load: the repos of workspace, or wsErr when
// there is no workspace to list.
func (a App) loadRepos(workspace string, wsErr error) func() tea.Cmd {
	ctx, prbc := a.ctx, a.s.d.PRB
	return func() tea.Cmd {
		return func() tea.Msg {
			if wsErr != nil {
				return screens.ReposLoadedMsg{Err: wsErr}
			}
			repos, err := prbc.Repos(ctx, workspace)
			return screens.ReposLoadedMsg{Repos: repos, Err: err}
		}
	}
}

// notifiedMsg is the result of the desktop notification of text; nothing
// waits on it.
type notifiedMsg struct {
	text string
	err  error
}

// notify tells the desktop that a review the user is not watching ended.
func (a App) notify(text string) tea.Cmd {
	s := a.sys
	return func() tea.Msg { return notifiedMsg{text: text, err: s.Notify(context.Background(), "pr-review", text)} }
}

// doneText says how a review ended. One the user cancelled reads as
// cancelled whatever error the engine ended with.
func doneText(msg screens.ReviewDoneMsg, cancelled bool) string {
	switch {
	case msg.Err == nil:
		t := fmt.Sprintf("#%d listo: %s", msg.PR, report.VerdictText(msg.Result))
		if msg.Warning != nil {
			t += " Aviso: " + msg.Warning.Error() + "."
		}
		return t
	case cancelled || errors.Is(msg.Err, context.Canceled):
		return fmt.Sprintf("#%d: la revisión se canceló.", msg.PR)
	}
	return fmt.Sprintf("#%d: la revisión falló: %v", msg.PR, msg.Err)
}

// saveConfig writes the config where DefaultDeps reads it.
func saveConfig(c config.Config) error {
	p, err := config.Path()
	if err != nil {
		return err
	}
	return config.Save(p, c)
}
