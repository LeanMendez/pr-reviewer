// Package listdata loads and classifies the open PRs of a repo for the list screen.
package listdata

import (
	"context"
	"path"
	"sort"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/eligibility"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/prechecks"
	"github.com/LeanMendez/pr-review/internal/report"
)

const defaultParallel = 6

// Row is one PR of the list. Reason is empty for ready PRs.
type Row struct {
	Ctx        prb.PRContext
	Lines      int
	Suggested  pipeline.Mode
	Reason     eligibility.Reason
	LastReview time.Time
	Reviewed   bool
}

// Loader builds the rows of a repo through prb, without downloading diffs.
type Loader struct {
	PRB         prb.Client
	Me          prb.Identity
	ReportsRoot string
	Cfg         config.Config
	Parallel    int // simultaneous `prb pr context` calls; 6 when zero
}

// Load lists the open PRs of repo ("workspace/slug"), fetches each context in
// parallel and splits them into ready and hidden, both newest first.
func (l Loader) Load(ctx context.Context, repo string) (ready []Row, hidden []Row, err error) {
	list, err := l.PRB.ListOpen(ctx, repo)
	if err != nil {
		return nil, nil, err
	}
	parallel := l.Parallel
	if parallel <= 0 {
		parallel = defaultParallel
	}
	ctxs := make([]prb.PRContext, len(list))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(parallel)
	for i, s := range list {
		g.Go(func() error {
			c, _, err := l.PRB.Context(gctx, repo, s.ID)
			if err != nil {
				return err
			}
			ctxs[i] = c
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	slug := path.Base(repo) // report dirs are <root>/<slug>/...
	for _, c := range ctxs {
		row := l.row(slug, c)
		if ok, reason := eligibility.Classify(c, l.Me); ok {
			ready = append(ready, row)
		} else {
			row.Reason = reason
			hidden = append(hidden, row)
		}
	}
	sortNewestFirst(ready)
	sortNewestFirst(hidden)
	return ready, hidden, nil
}

func (l Loader) row(slug string, c prb.PRContext) Row {
	var stats []prb.FileStat
	if c.Files != nil { // nil when prb's files section failed
		stats = c.Files.Files
	}
	pre := prechecks.Summarize(stats, l.Cfg)
	row := Row{Ctx: c, Lines: pre.TotalLines, Suggested: pipeline.Suggest(pre)}
	row.LastReview, row.Reviewed = report.LastReview(l.ReportsRoot, slug, c.PullRequest.ID)
	return row
}

func sortNewestFirst(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].Ctx.PullRequest, rows[j].Ctx.PullRequest
		if !a.UpdatedOn.Equal(b.UpdatedOn) {
			return a.UpdatedOn.After(b.UpdatedOn)
		}
		return a.ID > b.ID
	})
}
