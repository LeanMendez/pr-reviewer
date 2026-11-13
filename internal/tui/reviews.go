package tui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/app"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/tui/listdata"
	"github.com/LeanMendez/pr-review/internal/tui/screens"
)

// eventBuffer smooths the bursts of a review: stage and agent events wait
// for the reader, activity is dropped when the buffer is full.
const eventBuffer = 64

// ReviewCmd runs the review of row the way the review command does
// (app.Review) and reports it as a ReviewDoneMsg. It closes events when the
// review returns, so whoever reads them sees the end.
//
// Worktrees left behind are a warning on a finished review. So is a report
// that could not be written: the result is still there to show.
func ReviewCmd(ctx context.Context, d app.Deps, repo string, row listdata.Row, mode pipeline.Mode, events chan<- pipeline.Event) tea.Cmd {
	return func() tea.Msg {
		pr := row.Ctx.PullRequest.ID
		res, dir, err := app.Review(ctx, d, repo, pr, mode, events)
		close(events)
		var cleanup *app.CleanupError
		switch {
		case err == nil:
			return screens.ReviewDoneMsg{PR: pr, Result: res, Dir: dir}
		case errors.As(err, &cleanup):
			return screens.ReviewDoneMsg{PR: pr, Result: res, Dir: dir, Warning: cleanup}
		case res.Verdict != "":
			return screens.ReviewDoneMsg{PR: pr, Result: res, Warning: fmt.Errorf("no se pudo guardar el reporte: %w", err)}
		}
		return screens.ReviewDoneMsg{PR: pr, Err: err}
	}
}

// runFunc builds the command that runs one review; ReviewCmd with the
// current deps and repo.
type runFunc func(ctx context.Context, row listdata.Row, mode pipeline.Mode, events chan<- pipeline.Event) tea.Cmd

// review is one review started from the interface.
type review struct {
	cancel   context.CancelFunc
	events   chan pipeline.Event
	progress screens.ProgressModel
	done     *screens.ReviewDoneMsg // nil while it runs
	doneAt   time.Time
}

// Reviews is the registry of the reviews started in this session, by PR
// number: each with its cancel, its events channel, its progress screen and,
// once it ends, its result. It is only used from the program's Update.
type Reviews struct {
	base   context.Context
	run    runFunc
	buffer int
	byPR   map[int]*review
	wg     sync.WaitGroup
}

func newReviews(base context.Context, run runFunc) *Reviews {
	return &Reviews{base: base, run: run, buffer: eventBuffer, byPR: map[int]*review{}}
}

// Start launches the review of row in mode, with its own context. It
// returns the channel the review writes its events to, which the caller
// must drain until it is closed, and a command that delivers the
// ReviewDoneMsg. When a review of that PR is still running it starts
// nothing and returns a nil command.
//
// The review runs in its own goroutine from the start, so Wait knows about
// it even if the program never runs the command.
func (r *Reviews) Start(row listdata.Row, mode pipeline.Mode) (<-chan pipeline.Event, tea.Cmd) {
	pr := row.Ctx.PullRequest.ID
	if rv := r.byPR[pr]; rv != nil && rv.done == nil {
		return rv.events, nil
	}
	ctx, cancel := context.WithCancel(r.base)
	events := make(chan pipeline.Event, r.buffer)
	r.byPR[pr] = &review{cancel: cancel, events: events, progress: screens.NewProgress(row, mode, nil, cancel)}
	cmd := r.run(ctx, row, mode, events)
	result := make(chan tea.Msg, 1)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer cancel()
		result <- cmd()
	}()
	return events, func() tea.Msg { return <-result }
}

// get returns the review of pr, running or finished, or nil.
func (r *Reviews) get(pr int) *review { return r.byPR[pr] }

// running reports whether a review of pr is still running.
func (r *Reviews) running(pr int) bool {
	rv := r.byPR[pr]
	return rv != nil && rv.done == nil
}

// finish records the end of a review at at and returns it; nil when msg is
// not about a running review.
func (r *Reviews) finish(msg screens.ReviewDoneMsg, at time.Time) *review {
	rv := r.byPR[msg.PR]
	if rv == nil || rv.done != nil {
		return nil
	}
	rv.done, rv.doneAt = &msg, at
	return rv
}

// markSaved marks on list every review of this session that saved a report.
func (r *Reviews) markSaved(list screens.ListModel) screens.ListModel {
	for pr, rv := range r.byPR {
		if rv.done != nil && rv.done.Dir != "" {
			list = list.MarkReviewed(pr, rv.doneAt)
		}
	}
	return list
}

// Running is how many reviews have not ended yet.
func (r *Reviews) Running() int {
	n := 0
	for _, rv := range r.byPR {
		if rv.done == nil {
			n++
		}
	}
	return n
}

// CancelAll cancels every running review. Each still ends with its
// ReviewDoneMsg, after removing its worktrees.
func (r *Reviews) CancelAll() {
	for _, rv := range r.byPR {
		if rv.done == nil {
			rv.cancel()
		}
	}
}

// Wait blocks until every review started has returned, worktrees removed.
func (r *Reviews) Wait() { r.wg.Wait() }

// reviewEventMsg is an event read from the channel of the review of pr.
type reviewEventMsg struct {
	pr int
	ch <-chan pipeline.Event
	ev pipeline.Event
}

// drain reads the next event of ch; a closed channel yields no message and
// ends the reading.
func drain(pr int, ch <-chan pipeline.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return reviewEventMsg{pr: pr, ch: ch, ev: ev}
	}
}
