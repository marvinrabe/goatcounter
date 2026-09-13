package widgets

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/parse"
)

type Pages struct {
	id     int
	loaded bool
	err    error
	html   template.HTML

	RefsForPath      goatcounter.PathID
	Limit, LimitRefs int
	Display          int
	More             bool
	Pages            goatcounter.HitLists
	Refs             goatcounter.HitStats
	Max              int
	Exclude          []goatcounter.PathID
	WithStats        bool // Include per-page time series for API clients.
}

func (w Pages) Name() string { return "pages" }
func (w Pages) Type() string { return "full-width" }
func (w Pages) Label() string {
	return "Paths overview"
}
func (w *Pages) SetHTML(h template.HTML) { w.html = h }
func (w Pages) HTML() template.HTML      { return w.html }
func (w *Pages) SetErr(h error)          { w.err = h }
func (w Pages) Err() error               { return w.err }
func (w Pages) ID() int                  { return w.id }

func (w *Pages) SetDetail(d string) {
	w.RefsForPath, _ = parse.Int[goatcounter.PathID](d, 10)
}

func (w *Pages) GetData(ctx context.Context, a Args) (bool, error) {
	if w.RefsForPath > 0 {
		err := w.Refs.ListRefsByPathID(ctx, w.RefsForPath, a.Rng, w.LimitRefs, a.Offset)
		return w.Refs.More, err
	}

	var (
		wg      sync.WaitGroup
		refsErr error
	)
	if a.ShowRefs > 0 {
		wg.Go(func() {
			defer func() {
				if p := recover(); p != nil {
					slog.ErrorContext(ctx, "background task panic", "panic", p, "stack", string(debug.Stack()))
				}
			}()
			refsErr = w.Refs.ListRefsByPathID(ctx, a.ShowRefs, a.Rng, w.LimitRefs, a.Offset)
		})
	}

	var err error
	if w.WithStats {
		w.Display, w.More, err = w.Pages.List(ctx, a.Rng, a.PathFilter, w.Exclude, w.Limit, a.Group)
	} else {
		w.Display, w.More, err = w.Pages.ListCounts(ctx, a.Rng, a.PathFilter, w.Exclude, w.Limit)
	}

	wg.Wait()

	for _, p := range w.Pages {
		if p.Max > w.Max {
			w.Max = p.Max
		}
	}

	w.loaded = true
	return w.More, errors.Join(err, refsErr)
}

func (w Pages) RenderHTML(ctx context.Context, shared SharedData) (string, any) {
	if w.RefsForPath > 0 {
		return "_dashboard_pages_refs.gohtml", struct {
			Context context.Context
			Site    *goatcounter.Site
			ID      int
			Loaded  bool
			Err     error

			Refs  goatcounter.HitStats
			Count int
		}{ctx, shared.Site, w.id, w.loaded, w.err,
			w.Refs, shared.Total}
	}

	t := "_dashboard_pages_text"
	if shared.RowsOnly {
		t += "_rows"
	}
	t += ".gohtml"

	return t, struct {
		Context context.Context
		ID      int
		Loaded  bool
		Err     error
		Pages   goatcounter.HitLists
		Group   goatcounter.Group
		Max     int

		TotalDisplay int
		Total        int
		MorePages    bool

		Refs     goatcounter.HitStats
		ShowRefs goatcounter.PathID
	}{
		Context: ctx,
		ID:      w.id, Loaded: w.loaded, Err: w.err, Pages: w.Pages,
		Group: shared.Args.Group, Max: w.Max,
		TotalDisplay: w.Display, Total: shared.Total, MorePages: w.More,
		Refs: w.Refs, ShowRefs: shared.Args.ShowRefs,
	}
}
