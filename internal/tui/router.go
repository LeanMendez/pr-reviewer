package tui

import (
	"path"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/app"
	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/report"
	"github.com/LeanMendez/pr-review/internal/tui/listdata"
	"github.com/LeanMendez/pr-review/internal/tui/screens"
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
		return a, nil
	// Loads land on their screen even when the user already moved on.
	case screens.RowsLoadedMsg:
		a.list, cmd = update(a.list, msg)
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
	case screens.OpenModelsMsg:
		return a.openModels()
	case screens.ModelsSavedMsg:
		a.s.setConfig(msg.Cfg)
		if a.modelsOnly {
			return a, tea.Quit
		}
		return a.show(scrList)
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
	a.list = screens.NewList(repo, a.loadRows(repo)).WithThreshold(a.s.d.Cfg.Thresholds.MaxLines)
	return a.show(scrList, a.list.Init())
}

func (a App) openModels() (App, tea.Cmd) {
	a.models = screens.NewModels(a.s.d.Cfg, a.save)
	return a.show(scrModels)
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
// report; one in the background marks its row and notifies the desktop.
func (a App) reviewDone(msg screens.ReviewDoneMsg) (App, tea.Cmd) {
	rv := a.reviews.finish(msg)
	if rv == nil {
		return a, nil
	}
	if msg.Dir != "" {
		a.list = a.list.MarkReviewed(msg.PR, a.now())
	}
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
	return a, a.notify(msg)
}

// openResult shows the report of the review on screen, with where it was
// saved and any warning it ended with.
func (a App) openResult(msg screens.OpenResultMsg) (App, tea.Cmd) {
	savedAt, note := "", ""
	if rv := a.reviews.get(a.progressPR); rv != nil && rv.done != nil {
		if rv.done.Dir != "" {
			savedAt = filepath.Join(rv.done.Dir, "review.md")
		}
		if rv.done.Warning != nil {
			note = "Aviso: " + rv.done.Warning.Error()
		}
	}
	a.report = screens.NewReport(msg.Result, a.sys, savedAt).WithNote(note)
	return a.show(scrReport)
}

// openReport shows the newest saved report of a PR, from the same root the
// reviews write to.
func (a App) openReport(msg screens.OpenReportMsg) (App, tea.Cmd) {
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
		return a.show(scrList)
	case scrMode, scrProgress, scrReport:
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
// when the last one has ended (worktrees removed); keys do nothing while
// waiting.
func (a App) confirmKey(msg tea.KeyPressMsg) (App, tea.Cmd) {
	if a.quitting {
		return a, nil
	}
	switch msg.String() {
	case "s":
		a.confirming, a.quitting = false, true
		a.reviews.CancelAll()
		if a.reviews.Running() == 0 {
			return a, tea.Quit
		}
	case "n", "esc":
		a.confirming = false
	}
	return a, nil
}
