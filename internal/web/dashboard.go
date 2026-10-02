package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
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
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/web/widgets"
)

// The "Last …" period shortcuts.
var periods = []string{"day", "week", "month", "quarter", "half-year", "year"}

// The dashboard period when nothing is given in the query string.
const defaultPeriod = "week"

// The dashboard view is whatever is in the query string; there is nothing
// saved or configurable.
func (s *server) dashboard(w http.ResponseWriter, r *http.Request) error {
	ctx, q := r.Context(), r.URL.Query()

	rng, err := s.getPeriod(r)
	if err != nil {
		return err
	}
	group, allowGroups := getGroup(r, rng)
	args := widgets.NewArgs(s.store, s.query(r, rng), group)
	args.ShowRefs = q.Get("showrefs")

	wid := widgets.NewList()
	if err := s.loadWidgets(r, wid, args); err != nil {
		return err
	}
	args.Total = wid.Get("totals").(*widgets.Totals).Visits()

	for _, widget := range wid {
		if err := ctx.Err(); err != nil {
			return err
		}
		html, err := renderTemplate(widget.RenderHTML(args))
		if err != nil {
			slog.With("module", "dashboard").ErrorContext(ctx, "render dashboard widget",
				"error", err, "widget", widget.Name(), requestAttrs(r))
			html = "template rendering error: " + template.HTMLEscapeString(err.Error())
		}
		widget.SetHTML(template.HTML(html))
	}

	rng = rng.In(s.store.Timezone.Loc())

	// When reloading the dashboard from e.g. the filter we don't need to render
	// header/footer/menu, etc. Render just the widgets and return that as JSON.
	if q.Get("reload") != "" {
		t, err := renderTemplate("_dashboard_widgets.gohtml", struct {
			Widgets widgets.List
			Cards   []widgets.Card
		}{wid, widgets.Cards})
		if err != nil {
			return err
		}
		return writeJSON(w, map[string]any{
			"widgets":   t,
			"timerange": rng.String(),
			"total":     args.Total,
		})
	}

	countDomain := r.Host
	return renderHTML(w, "dashboard.gohtml", struct {
		Globals
		CountDomain string
		Period      datetime.Range
		Periods     []string
		HLPeriod    string
		Group       analytics.Group
		AllowGroups analytics.Groups
		Filter      string
		ShowRefs    string
		Total       int
		Widgets     widgets.List
		Cards       []widgets.Card
	}{s.globals(r), countDomain, rng, periods, s.highlightedPeriod(rng), group, allowGroups,
		q.Get("filter"), args.ShowRefs, args.Total, wid, widgets.Cards})
}

// loadWidget loads more rows of a widget, or the detail of one of its rows.
func (s *server) loadWidget(w http.ResponseWriter, r *http.Request) error {
	ctx, q := r.Context(), r.URL.Query()

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
	wid := widgets.New(q.Get("widget"))
	if wid == nil {
		return httpErrorf(400, `unknown widget: %q`, q.Get("widget"))
	}
	wid.SetDetail(q.Get("key"))

	group, _ := getGroup(r, rng)
	args := widgets.NewArgs(s.store, s.query(r, rng), group)
	args.Offset, args.Total, args.RowsOnly = offset, total, offset > 0

	more, err := wid.GetData(ctx, args)
	if err != nil {
		return err
	}
	html, err := renderTemplate(wid.RenderHTML(args))
	if err != nil {
		return err
	}
	return writeJSON(w, map[string]any{"html": html, "more": more})
}

// loadWidgets runs the widget queries concurrently. Cancellation reaches
// every query, and waiting for all of them prevents rendering partially
// written data.
func (s *server) loadWidgets(r *http.Request, list widgets.List, args widgets.Args) error {
	ctx := r.Context()
	log := slog.With("module", "dashboard")
	var wg sync.WaitGroup
	for _, widget := range list {
		wg.Go(func() {
			defer func() {
				if p := recover(); p != nil {
					log.ErrorContext(ctx, "widget panic", "panic", p, "stack", string(debug.Stack()),
						"widget", widget.Name(), requestAttrs(r))
					_, userErr := userError(fmt.Errorf("widget panic: %v", p))
					widget.SetErr(userErr)
				}
			}()
			wctx, cancel := context.WithTimeout(ctx, dashTimeout)
			defer cancel()
			start := time.Now()
			_, err := widget.GetData(wctx, args)
			switch {
			case err == nil:
			case errors.Is(err, context.DeadlineExceeded):
				log.WarnContext(ctx, "dashboard widget timed out", "error", err, "widget", widget.Name(), requestAttrs(r))
				_, userErr := userError(err)
				widget.SetErr(userErr)
			case errors.Is(err, context.Canceled):
				widget.SetErr(err)
			default:
				log.ErrorContext(ctx, "load dashboard widget", "error", err, "widget", widget.Name(), requestAttrs(r))
				_, userErr := userError(err)
				widget.SetErr(userErr)
			}
			log.DebugContext(ctx, widget.Name(), "took", time.Since(start))
		})
	}
	wg.Wait()
	return ctx.Err()
}

// highlightedPeriod is the "Last …" shortcut that describes rng, if any.
func (s *server) highlightedPeriod(rng datetime.Range) string {
	for _, p := range periods {
		if want := s.lastPeriod(p); rng.Start.Equal(want.Start) && rng.End.Equal(want.End) {
			return p
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
func (s *server) lastPeriod(period string) datetime.Range {
	rng := datetime.NewRange(datetime.Now().In(s.store.Timezone.Loc())).Current(datetime.Day)
	switch period {
	case "week":
		rng = rng.Last(datetime.Week(false))
	case "month":
		rng = rng.Last(datetime.Month)
	case "quarter":
		rng = rng.Last(datetime.Quarter)
	case "half-year":
		rng = rng.Last(datetime.HalfYear)
	case "year":
		rng = rng.Last(datetime.Year)
	}
	return rng.UTC()
}

// getPeriod gets the period from the query string: a "Last …" shortcut in
// "period", or the dates in "period-start" and "period-end".
func (s *server) getPeriod(r *http.Request) (datetime.Range, error) {
	var (
		q   = r.URL.Query()
		loc = s.store.Timezone.Loc()
		rng datetime.Range
	)
	if p := q.Get("period"); slices.Contains(periods, p) {
		return s.lastPeriod(p), nil
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
func (s *server) query(r *http.Request, rng datetime.Range) analytics.Query {
	return analytics.Query{
		Site:   siteFrom(r.Context()),
		Range:  rng,
		Filter: analytics.NewPathFilter(r.URL.Query().Get("filter")),
	}
}

// getGroup gets the chart grouping from the query string, and the groupings
// that can be selected for this period.
func getGroup(r *http.Request, rng datetime.Range) (analytics.Group, analytics.Groups) {
	// Viewing by hour for a year or viewing by day for 2 days looks horrible,
	// so don't allow that sort of thing.
	//
	// These numbers are based on what makes sense when you click "Last day ·
	// week · month · quarter · half year · year", which is probably what most
	// people use.
	var allow analytics.Groups
	switch d := rng.End.Sub(rng.Start).Hours() / 24; {
	case d <= 6:
		allow = analytics.Groups{analytics.GroupHourly}
	case d < 90:
		allow = analytics.Groups{analytics.GroupHourly, analytics.GroupDaily}
	case d < 364:
		allow = analytics.Groups{analytics.GroupDaily, analytics.GroupWeekly}
	default:
		allow = analytics.Groups{analytics.GroupDaily, analytics.GroupWeekly, analytics.GroupMonthly}
	}

	// Keep the full date range, but avoid rendering millions of empty points.
	group := analytics.ChartGroup(rng, allow[0])
	allow = slices.DeleteFunc(allow, func(g analytics.Group) bool { return g < group })
	if len(allow) == 0 {
		allow = analytics.Groups{group}
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
