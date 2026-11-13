// Package listdata loads and classifies the open PRs of a repo for the list screen.
package listdata

import (
	"context"
	"path"
	"slices"
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

// Load lists every open PR of repo ("workspace/slug"), fetches each context
// in parallel and splits them into ready and hidden, both newest first.
func (l Loader) Load(ctx context.Context, repo string) (ready []Row, hidden []Row, err error) {
	ready, hidden, _, err = l.LoadPage(ctx, repo, 0, nil)
	return ready, hidden, err
}

// LoadPage is Load for the limit most recently updated open PRs (all when
// limit is 0), skipping the ones in have: their contexts, the slow part, are
// not fetched again. more says whether prb may have older PRs, which is when
// it returned the whole limit.
func (l Loader) LoadPage(ctx context.Context, repo string, limit int, have map[int]bool) (ready, hidden []Row, more bool, err error) {
	list, err := l.PRB.ListOpen(ctx, repo, limit)
	if err != nil {
		return nil, nil, false, err
	}
	more = limit > 0 && len(list) >= limit
	list = slices.DeleteFunc(list, func(s prb.PRSummary) bool { return have[s.ID] })
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
		return nil, nil, false, err
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
	return ready, hidden, more, nil
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
