package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/web/widgets"
)

// The "Last …" period shortcuts.
var periods = []analytics.Period{analytics.Day, analytics.Week, analytics.Month,
	analytics.Quarter, analytics.HalfYear, analytics.Year}

// The dashboard period when nothing is given in the query string.
const defaultPeriod = analytics.Week

// dashWidgets are the totals and the cards below them.
type dashWidgets struct {
	Totals struct {
		analytics.DashboardData
		Err error
	}
	Cards  []widgets.Card
	Panels map[string]widgets.Panel
}

// The dashboard view is whatever is in the query string; there is nothing
// saved or configurable.
func (s *server) dashboard(w http.ResponseWriter, r *http.Request) error {
	ctx, q := r.Context(), r.URL.Query()

	rng, err := s.getPeriod(r)
	if err != nil {
		return err
	}
	group, allowGroups := getGroup(r, rng)
	query := s.query(r, rng, group)
	showRefs := q.Get("showrefs")

	// Run the queries concurrently. Cancellation reaches every query, and
	// waiting for all of them prevents rendering partially written data.
	var (
		wg    sync.WaitGroup
		dash  = dashWidgets{Cards: widgets.Cards, Panels: map[string]widgets.Panel{}}
		kinds = widgets.Kinds()
		data  = make([]analytics.Breakdown, len(kinds))
		refs  analytics.Breakdown
		errs  = make([]error, len(kinds))
	)
	wg.Go(func() {
		dash.Totals.Err = s.loadWidget(r, "totals", func(ctx context.Context) (err error) {
			dash.Totals.DashboardData, err = s.store.Dashboard(ctx, query, group)
			return err
		})
	})
	for i, k := range kinds {
		wg.Go(func() {
			errs[i] = s.loadWidget(r, k.Name, func(ctx context.Context) (err error) {
				data[i], err = k.Load(ctx, s.store, query, "", 0)
				if err == nil && k.Pages && showRefs != "" {
					refs, err = k.Load(ctx, s.store, query, showRefs, 0)
				}
				return err
			})
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}

	total := dash.Totals.Metrics.Visits
	for i, k := range kinds {
		p := widgets.Panel{Kind: k, Err: errs[i], Chart: k.Chart(data[i], "", total)}
		if k.Pages && showRefs != "" {
			p.Chart.Expand(showRefs, func(count int) widgets.Chart { return k.Chart(refs, showRefs, count) })
		}
		dash.Panels[k.Name] = p
	}

	rng = rng.In(s.store.Timezone.Loc())

	// When reloading the dashboard from e.g. the filter we don't need to render
	// header/footer/menu, etc. Render just the widgets and return that as JSON.
	if q.Get("reload") != "" {
		t, err := renderTemplate("_dashboard_widgets.gohtml", dash)
		if err != nil {
			return err
		}
		return writeJSON(w, map[string]any{
			"widgets":   t,
			"timerange": rng.String(),
			"total":     total,
		})
	}

	countDomain := r.Host
	return renderHTML(w, "dashboard.gohtml", struct {
		Globals
		dashWidgets
		CountDomain string
		Period      analytics.Range
		Periods     []analytics.Period
		HLPeriod    string
		Group       analytics.Period
		AllowGroups []analytics.Period
		Filter      string
		ShowRefs    string
		Total       int
	}{s.globals(r), dash, countDomain, rng, periods, s.highlightedPeriod(rng), group, allowGroups,
		q.Get("filter"), showRefs, total})
}

// loadRows loads more rows of a chart, or the detail of one of its rows.
func (s *server) loadRows(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()

	total, err := intParam(q, "total")
	if err != nil {
		return err
	}
	offset, err := intParam(q, "offset")
	if err != nil {
		return err
	}
	rng, err := s.getPeriod(r)
	if err != nil {
		return err
	}
	k, ok := widgets.Find(q.Get("widget"))
	if !ok {
		return httpErrorf(400, `unknown widget: %q`, q.Get("widget"))
	}
	key := q.Get("key")
	if key != "" && k.Detail == "" {
		return httpErrorf(400, `widget %q has no detail`, k.Name)
	}

	group, _ := getGroup(r, rng)
	b, err := k.Load(r.Context(), s.store, s.query(r, rng, group), key, offset)
	if err != nil {
		return err
	}
	chart := k.Chart(b, key, total)
	chart.RowsOnly = offset > 0

	var html string
	if key != "" && !chart.RowsOnly {
		html, err = renderTemplate("_chart_detail", widgets.ChartRow{Key: key, Count: total, Detail: &chart})
	} else {
		html, err = renderTemplate("_chart.gohtml", chart)
	}
	if err != nil {
		return err
	}
	return writeJSON(w, map[string]any{"html": html, "more": b.More})
}

// loadWidget runs one of the dashboard queries, and gets the error to show in
// the widget instead of its data.
func (s *server) loadWidget(r *http.Request, name string, load func(context.Context) error) (userErr error) {
	ctx := r.Context()
	log := slog.With("module", "dashboard")
	defer func() {
		if p := recover(); p != nil {
			log.ErrorContext(ctx, "widget panic", "panic", p, "stack", string(debug.Stack()),
				"widget", name, requestAttrs(r))
			_, userErr = userError(fmt.Errorf("widget panic: %v", p))
		}
	}()
	wctx, cancel := context.WithTimeout(ctx, dashTimeout)
	defer cancel()
	start := time.Now()
	defer func() { log.DebugContext(ctx, name, "took", time.Since(start)) }()

	err := load(wctx)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded):
		log.WarnContext(ctx, "dashboard widget timed out", "error", err, "widget", name, requestAttrs(r))
	case errors.Is(err, context.Canceled):
		return err
	default:
		log.ErrorContext(ctx, "load dashboard widget", "error", err, "widget", name, requestAttrs(r))
	}
	_, userErr = userError(err)
	return userErr
}

// highlightedPeriod is the "Last …" shortcut that describes rng, if any.
func (s *server) highlightedPeriod(rng analytics.Range) string {
	for _, p := range periods {
		if want := s.lastPeriod(p); rng.Start.Equal(want.Start) && rng.End.Equal(want.End) {
			return p.String()
		}
	}
	return ""
}

// lastPeriod gets the range for a "Last …" shortcut: from exactly this
// period ago to the end of today.
//
// The return value is always in UTC, and is the UTC day range corresponding
// to the configured timezone. So, for example a week in +08:00 would be:
// 2020-12-20 16:00:00 - 2020-12-27 15:59:59
func (s *server) lastPeriod(p analytics.Period) analytics.Range {
	rng := analytics.NewRange(time.Now().In(s.store.Timezone.Loc())).Current(analytics.Day)
	if p != analytics.Day {
		rng = rng.Last(p)
	}
	return rng.UTC()
}

// getPeriod gets the period from the query string: a "Last …" shortcut in
// "period", or the dates in "period-start" and "period-end".
func (s *server) getPeriod(r *http.Request) (analytics.Range, error) {
	var (
		q   = r.URL.Query()
		loc = s.store.Timezone.Loc()
		rng analytics.Range
	)
	if i := slices.IndexFunc(periods, func(p analytics.Period) bool { return p.String() == q.Get("period") }); i >= 0 {
		return s.lastPeriod(periods[i]), nil
	}
	if d := q.Get("period-start"); d != "" {
		var err error
		rng.Start, err = time.ParseInLocation("2006-01-02", d, loc)
		if err != nil {
			return rng, httpError(400, "Invalid start date: "+d)
		}
	}
	if d := q.Get("period-end"); d != "" {
		var err error
		rng.End, err = time.ParseInLocation("2006-01-02 15:04:05", d+" 23:59:59", loc)
		if err != nil {
			return rng, httpError(400, "Invalid end date: "+d)
		}
	}

	if q.Get("period-start") == "" || q.Get("period-end") == "" {
		return s.lastPeriod(defaultPeriod), nil
	}
	if rng.End.Before(rng.Start) {
		return rng, httpError(400, "end date is before start date")
	}
	return rng.From(rng.Start).To(rng.End).UTC(), nil
}

// query gets the dashboard's query for the request's site.
func (s *server) query(r *http.Request, rng analytics.Range, group analytics.Period) analytics.Query {
	// Align to start of week or month if we're grouping by week or month.
	//
	// This gives a really jarring experience if the UI is updated with the new
	// dates, as switching between day/week/month can really move the date
	// around. So don't update the UI and just "silently" include the extra date
	// ranges.
	if group == analytics.Week || group == analytics.Month {
		loc := s.store.Timezone.Loc()
		rng.Start = analytics.StartOf(rng.Start.In(loc), group).UTC()
		rng.End = analytics.EndOf(rng.End.In(loc), group).UTC()
	}
	return analytics.Query{
		Site:   siteFrom(r.Context()),
		Range:  rng,
		Filter: analytics.NewPathFilter(r.URL.Query().Get("filter")),
	}
}

// getGroup gets the chart grouping from the query string, and the groupings
// that can be selected for this period.
func getGroup(r *http.Request, rng analytics.Range) (analytics.Period, []analytics.Period) {
	// Viewing by hour for a year or viewing by day for 2 days looks horrible,
	// so don't allow that sort of thing.
	//
	// These numbers are based on what makes sense when you click "Last day ·
	// week · month · quarter · half year · year", which is probably what most
	// people use.
	var allow []analytics.Period
	switch d := rng.End.Sub(rng.Start).Hours() / 24; {
	case d <= 6:
		allow = []analytics.Period{analytics.Hour}
	case d < 90:
		allow = []analytics.Period{analytics.Hour, analytics.Day}
	case d < 364:
		allow = []analytics.Period{analytics.Day, analytics.Week}
	default:
		allow = []analytics.Period{analytics.Day, analytics.Week, analytics.Month}
	}

	// Keep the full date range, but avoid rendering millions of empty points.
	group := analytics.ChartGroup(rng, allow[0])
	allow = slices.DeleteFunc(allow, func(g analytics.Period) bool { return g < group })
	if len(allow) == 0 {
		allow = []analytics.Period{group}
	}
	// By day reads best when it's allowed, such as for the last week.
	if slices.Contains(allow, analytics.Day) {
		group = analytics.Day
	}

	want := strings.ToLower(r.URL.Query().Get("group"))
	for _, g := range allow {
		if g.String() == want {
			group = g
		}
	}
	return group, allow
}

// intParam parses an optional integer query parameter.
func intParam(q url.Values, key string) (int, error) {
	v := strings.TrimSpace(q.Get(key))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, httpErrorf(400, "%s: must be a whole number", key)
	}
	return n, nil
}
