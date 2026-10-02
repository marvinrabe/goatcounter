package widgets

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/marvinrabe/goatcounter/internal/analytics"
)

// Pages lists the visits for every path, and the referrers for one of them.
type Pages struct {
	base

	RefsForPath string
	More        bool
	Pages       analytics.HitLists
	Refs        analytics.HitStats
}

func (w *Pages) SetDetail(d string) { w.RefsForPath = d }

func (w *Pages) GetData(ctx context.Context, a Args) (bool, error) {
	if w.RefsForPath != "" {
		err := w.Refs.ListVisitorBreakdown(ctx, "pagerefs", w.RefsForPath, a.Rng, a.PathFilter, refPageSize, a.Offset)
		return w.Refs.More, err
	}

	var (
		wg      sync.WaitGroup
		refsErr error
	)
	if a.ShowRefs != "" {
		wg.Go(func() {
			defer func() {
				if p := recover(); p != nil {
					slog.ErrorContext(ctx, "background task panic", "panic", p, "stack", string(debug.Stack()))
				}
			}()
			refsErr = w.Refs.ListVisitorBreakdown(ctx, "pagerefs", a.ShowRefs, a.Rng, a.PathFilter, refPageSize, 0)
		})
	}

	var err error
	_, w.More, err = w.Pages.ListVisitorPages(ctx, a.Rng, a.PathFilter, pageSize, a.Offset)
	wg.Wait()
	return w.More, errors.Join(err, refsErr)
}

func (w Pages) RenderHTML(ctx context.Context, a Args) (string, any) {
	if w.RefsForPath != "" {
		return "_chart.gohtml", newChart(w.Refs, a.Total, false, nameDirect, a.RowsOnly)
	}
	chart := pagesChart(w.Pages, a.Total, a.ShowRefs, w.Refs, a.RowsOnly)
	if a.RowsOnly {
		return "_chart.gohtml", chart
	}
	return "_dashboard_pages.gohtml", struct {
		Err   error
		Chart Chart
		More  bool
	}{w.err, chart, w.More}
}
