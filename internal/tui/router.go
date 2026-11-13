package tui

import (
	"path"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/app"
	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/report"
	"github.com/LeanMendez/pr-review/internal/tui/listdata"
	"github.com/LeanMendez/pr-review/internal/tui/screens"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// Update routes msg: what changes screens or belongs to a review is handled
// here, the rest goes to the active screen.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return a.route(msg) }

func (a App) route(msg tea.Msg) (App, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a.forward(msg)
	case tea.KeyPressMsg:
		if a.confirming || a.quitting {
			return a.confirmKey(msg)
		}
	case screens.QuitMsg:
		return a.quit()
	case reviewEventMsg:
		return a.reviewEvent(msg)
	case screens.ReviewDoneMsg:
		return a.reviewDone(msg)
	case notifiedMsg:
		if msg.err != nil {
			a.list = a.list.WithNote(msg.text + " (No pude avisar: " + msg.err.Error() + ")")
		}
		return a, nil
	// Loads land on their screen even when the user already moved on.
	case screens.RowsLoadedMsg:
		a.list, cmd = update(a.list, msg)
		// A load started before a review ended does not know about it.
		a.list = a.reviews.mark(a.list)
		return a, cmd
	case screens.ReposLoadedMsg:
		a.picker, cmd = update(a.picker, msg)
		return a, cmd
	}
	if a.screen == scrOnboarding {
		if done, ok := msg.(screens.OnboardingDoneMsg); ok {
			return a.onboarded(done.Cfg)
		}
		// The setup nests the preset picker and the models table: their
		// messages come back to it through the program loop.
		return a.forward(msg)
	}
	switch msg := msg.(type) {
	case screens.RepoChosenMsg:
		return a.openList(msg.Repo)
	case screens.OpenModeMsg:
		return a.openMode(msg.Row)
	case screens.StartReviewMsg:
		return a.startReview(msg)
	case screens.BackgroundMsg:
		return a.show(scrList)
	case screens.OpenResultMsg:
		return a.openResult(msg)
	case screens.OpenReportMsg:
		return a.openReport(msg)
	case screens.OpenSettingsMsg:
		a.settings = screens.NewSettings()
		return a.show(scrSettings)
	case screens.OpenModelsMsg:
		return a.openModels()
	case screens.ModelsSavedMsg:
		a.s.setConfig(msg.Cfg)
		if a.modelsOnly {
			return a, tea.Quit
		}
		return a.show(scrSettings)
	case screens.OpenThemesMsg:
		a.themes = screens.NewThemes(a.s.d.Cfg.Tema)
		return a.show(scrThemes)
	case screens.ThemeChosenMsg:
		return a.chooseTheme(msg.Name)
	case screens.BackMsg:
		return a.back()
	}
	return a.forward(msg)
}

// update runs msg through a screen of a known type.
func update[M tea.Model](m M, msg tea.Msg) (M, tea.Cmd) {
	n, cmd := m.Update(msg)
	return n.(M), cmd
}

// forward gives msg to the active screen.
func (a App) forward(msg tea.Msg) (App, tea.Cmd) {
	var cmd tea.Cmd
	switch a.screen {
	case scrOnboarding:
		a.onboarding, cmd = update(a.onboarding, msg)
	case scrPicker:
		a.picker, cmd = update(a.picker, msg)
	case scrList:
		a.list, cmd = update(a.list, msg)
	case scrMode:
		a.mode, cmd = update(a.mode, msg)
	case scrProgress:
		if rv := a.reviews.get(a.progressPR); rv != nil {
			rv.progress, cmd = update(rv.progress, msg)
		}
	case scrReport:
		a.report, cmd = update(a.report, msg)
	case scrModels:
		a.models, cmd = update(a.models, msg)
	case scrSettings:
		a.settings, cmd = update(a.settings, msg)
	case scrThemes:
		a.themes, cmd = update(a.themes, msg)
	}
	return a, cmd
}

// show makes id the active screen, sized to the terminal, and runs cmds.
func (a App) show(id screen, cmds ...tea.Cmd) (App, tea.Cmd) {
	a.screen = id
	if a.width > 0 || a.height > 0 {
		var cmd tea.Cmd
		a, cmd = a.forward(tea.WindowSizeMsg{Width: a.width, Height: a.height})
		cmds = append(cmds, cmd)
	}
	return a, tea.Batch(cmds...)
}

func (a App) openOnboarding() (App, tea.Cmd) {
	a.onboarding = screens.NewOnboarding(screens.OnboardingDeps{PRB: a.s.d.PRB, Exec: a.s.d.Exec, Save: a.save})
	return a.show(scrOnboarding, a.onboarding.Init())
}

// onboarded takes the config the setup saved and opens what the user asked
// for: the list of the repo given, or the picker.
func (a App) onboarded(cfg config.Config) (App, tea.Cmd) {
	a.s.setConfig(cfg)
	if a.modelsOnly {
		return a, tea.Quit
	}
	if a.s.repo == "" {
		return a.openPicker()
	}
	repo, err := app.ResolveRepo(a.s.repo, cfg.Workspace)
	if err != nil {
		return a.openPicker()
	}
	return a.openList(repo)
}

func (a App) openPicker() (App, tea.Cmd) {
	ws, err := app.Workspace(a.s.d.Cfg.Workspace)
	a.picker = screens.NewRepoPicker(ws, a.loadRepos(ws, err))
	return a.show(scrPicker, a.picker.Init())
}

func (a App) openList(repo string) (App, tea.Cmd) {
	a.s.repo = repo
	a.list = a.reviews.mark(screens.NewList(repo, a.loadRows(repo)).WithThreshold(a.s.d.Cfg.Thresholds.MaxLines))
	if a.themeNote != "" {
		a.list, a.themeNote = a.list.WithNote(a.themeNote), ""
	}
	return a.show(scrList, a.list.Init())
}

func (a App) openModels() (App, tea.Cmd) {
	a.models = screens.NewModels(a.s.d.Cfg, a.save)
	return a.show(scrModels)
}

// chooseTheme saves the config with the theme name and, once saved, paints
// the whole interface with it and goes back to the settings. When the save
// fails the picker says so and nothing changes.
func (a App) chooseTheme(name string) (App, tea.Cmd) {
	th, ok := theme.ByName(name)
	if !ok {
		return a, nil
	}
	cfg := a.s.d.Cfg
	cfg.Tema = name
	if err := a.save(cfg); err != nil {
		a.themes = a.themes.WithSaveError(err)
		return a, nil
	}
	a.s.setConfig(cfg)
	theme.Use(th)
	return a.show(scrSettings)
}

// openMode shows the mode picker for row; a PR under review shows its
// progress instead, so it is never reviewed twice at once.
func (a App) openMode(row listdata.Row) (App, tea.Cmd) {
	if pr := row.Ctx.PullRequest.ID; a.reviews.running(pr) {
		return a.showProgress(pr)
	}
	a.mode = screens.NewMode(row, a.s.d.Cfg)
	return a.show(scrMode)
}

func (a App) showProgress(pr int) (App, tea.Cmd) {
	rv := a.reviews.get(pr)
	if rv == nil {
		return a, nil
	}
	a.progressPR = pr
	return a.show(scrProgress, rv.progress.Init())
}

// startReview launches the review and starts draining its events, which
// goes on until the review closes the channel, whatever screen is shown.
func (a App) startReview(msg screens.StartReviewMsg) (App, tea.Cmd) {
	pr := msg.Row.Ctx.PullRequest.ID
	events, wait := a.reviews.Start(msg.Row, msg.Mode)
	a.list = a.reviews.mark(a.list)
	a, cmd := a.showProgress(pr)
	if wait == nil { // already running, and already drained
		return a, cmd
	}
	return a, tea.Batch(cmd, wait, drain(pr, events))
}

// reviewEvent applies an event to its review's progress, shown or not, and
// reads the next one. Events of a channel that is no longer the review's
// (a finished review of the same PR) are only drained.
func (a App) reviewEvent(msg reviewEventMsg) (App, tea.Cmd) {
	if rv := a.reviews.get(msg.pr); rv != nil && rv.events == msg.ch {
		rv.progress, _ = update(rv.progress, screens.EventMsg(msg.ev))
	}
	return a, drain(msg.pr, msg.ch)
}

// reviewDone records the end of a review. A review on screen goes on to its
// report; one in the background marks its row and notifies the desktop, and
// when it did not end well (failed, cancelled, unsaved) the list says so too.
func (a App) reviewDone(msg screens.ReviewDoneMsg) (App, tea.Cmd) {
	rv := a.reviews.finish(msg, a.now())
	if rv == nil {
		return a, nil
	}
	a.list = a.reviews.mark(a.list)
	if a.quitting {
		if a.reviews.Running() == 0 {
			return a, tea.Quit
		}
		return a, nil
	}
	if a.confirming && a.reviews.Running() == 0 {
		a.confirming = false // nothing left to ask about
	}
	var cmd tea.Cmd
	rv.progress, cmd = update(rv.progress, msg)
	if a.screen == scrProgress && a.progressPR == msg.PR {
		return a, cmd
	}
	text := doneText(msg, rv.cancelled)
	if msg.Err != nil || msg.Warning != nil {
		a.list = a.list.WithNote(text)
	}
	return a, a.notify(text)
}

// openResult shows the report of the review on screen.
func (a App) openResult(msg screens.OpenResultMsg) (App, tea.Cmd) {
	return a.showResult(msg.Result, a.reviews.get(a.progressPR))
}

// showResult shows res, the result of rv, with where it was saved and any
// warning it ended with.
func (a App) showResult(res pipeline.Result, rv *review) (App, tea.Cmd) {
	savedAt, note := "", ""
	if rv != nil && rv.done != nil {
		if rv.done.Dir != "" {
			savedAt = filepath.Join(rv.done.Dir, "review.md")
		}
		if rv.done.Warning != nil {
			note = "Aviso: " + rv.done.Warning.Error()
		}
	}
	a.report = screens.NewReport(res, a.sys, savedAt).WithNote(note)
	return a.show(scrReport)
}

// openReport shows the last result of a PR: the one of this session, kept
// in memory even when it could not be saved, or else the newest saved
// report, from the same root the reviews write to.
func (a App) openReport(msg screens.OpenReportMsg) (App, tea.Cmd) {
	if rv := a.reviews.lastResult(msg.PR); rv != nil {
		return a.showResult(rv.done.Result, rv)
	}
	res, savedAt, err := report.LoadLatest(a.s.root, path.Base(msg.Repo), msg.PR)
	if err != nil {
		a.list = a.list.WithNote("No pude abrir el reporte: " + err.Error())
		return a, nil
	}
	a.report = screens.NewReport(res, a.sys, savedAt)
	return a.show(scrReport)
}

func (a App) back() (App, tea.Cmd) {
	switch a.screen {
	case scrModels:
		if a.modelsOnly {
			return a, tea.Quit
		}
		return a.show(scrSettings)
	case scrThemes:
		return a.show(scrSettings)
	case scrMode, scrProgress, scrReport, scrSettings:
		return a.show(scrList)
	}
	return a, nil
}

// quit leaves at once when nothing runs; otherwise it asks first.
func (a App) quit() (App, tea.Cmd) {
	switch {
	case a.confirming || a.quitting:
		return a, nil
	case a.reviews.Running() == 0:
		return a, tea.Quit
	}
	a.confirming = true
	return a, nil
}

// confirmKey answers the quit question. Yes cancels every review and quits
// when the last one has ended (worktrees removed). While asking or waiting,
// a second ctrl+c quits at once: Run then cancels the reviews without
// waiting for their cleanup.
func (a App) confirmKey(msg tea.KeyPressMsg) (App, tea.Cmd) {
	switch k := msg.String(); {
	case k == "ctrl+c":
		a.forced = true
		return a, tea.Quit
	case a.quitting: // waiting: only ctrl+c does something
	case k == "s":
		a.confirming, a.quitting = false, true
		a.reviews.CancelAll()
		if a.reviews.Running() == 0 {
			return a, tea.Quit
		}
	case k == "n" || k == "esc":
		a.confirming = false
	}
	return a, nil
}
