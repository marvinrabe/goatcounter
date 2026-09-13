package handlers

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/httpx"
	"github.com/marvinrabe/goatcounter/internal/parse"
	"github.com/marvinrabe/goatcounter/internal/validation"
	"github.com/marvinrabe/goatcounter/internal/widgets"
)

// The dashboard period when nothing is given in the query string.
const defaultPeriod = "week"

func (h backend) dashboard(w http.ResponseWriter, r *http.Request) error {
	site := Site(r.Context())
	q := r.URL.Query()

	// The dashboard view is whatever is in the query string; there is nothing
	// saved or configurable.
	period := strings.TrimSuffix(q.Get("hl-period"), "-cur")
	if period == "" {
		period = defaultPeriod
	}
	filter := q.Get("filter")

	rng, err := getPeriod(r)
	if err != nil {
		return err
	}
	if q.Get("period-start") == "" || q.Get("period-end") == "" {
		period = defaultPeriod
	}

	showRefs, _ := parse.Int[goatcounter.PathID](q.Get("showrefs"), 10)
	var allowGroups goatcounter.Groups
	group, allowGroups := getGroup(r, 0, rng)

	args := widgets.NewArgs(r.Context(), rng, group, allowGroups, showRefs)
	if filter != "" {
		start := time.Now()
		args.PathFilter, err = goatcounter.PathFilterFromQuery(r.Context(), filter)
		if err != nil {
			return err
		}
		slog.With("module", "dashboard").DebugContext(r.Context(), "pathfilter", "took", time.Since(start))
	}

	cfg := goatcounter.Config(r.Context())
	cd := cfg.DomainStatic
	if cd == "" {
		cd = r.Host + cfg.BasePath
	}

	// Load widgets data from the database.
	wid := widgets.NewList(r.Context())
	shared := widgets.SharedData{Args: args, Site: site}

	getHTML := func(w widgets.Widget) {
		tplName, tplData := w.RenderHTML(r.Context(), shared)
		if tplName == "" { // Some data doesn't have a template.
			return
		}
		tpl, err := renderTemplate(tplName, tplData)
		if err != nil {
			slog.With("module", "dashboard").ErrorContext(r.Context(), err.Error(), requestAttrs(r))
			w.SetHTML(template.HTML("template rendering error: " + template.HTMLEscapeString(err.Error())))
			return
		}

		w.SetHTML(template.HTML(tpl))
	}

	if err := h.loadDashboardWidgets(r, wid, args); err != nil {
		return err
	}

	// Set shared params.
	tc := wid.GetOne("totalcount").(*widgets.TotalCount)
	shared.Total, shared.TotalUTC, shared.TotalEvents = tc.Total, tc.TotalUTC, tc.TotalEvents
	shared.Metrics = tc.Metrics

	// All queries are finished before their results are read or rendered.
	for _, widget := range wid {
		if err := r.Context().Err(); err != nil {
			return err
		}
		getHTML(widget)
	}

	rng = rng.In(goatcounter.Config(r.Context()).Timezone.Loc())

	// When reloading the dashboard from e.g. the filter we don't need to render
	// header/footer/menu, etc. Render just the widgets and return that as JSON.
	if q.Get("reload") != "" {
		t, err := renderTemplate("_dashboard_widgets.gohtml", struct {
			Globals
			Widgets widgets.List
		}{newGlobals(r), wid})
		if err != nil {
			return err
		}

		return httpx.JSON(w, map[string]string{
			"widgets":   t,
			"timerange": rng.String(),
		})
	}

	return renderHTML(w, "dashboard.gohtml", struct {
		Globals
		CountDomain string
		ShowRefs    goatcounter.PathID
		Period      datetime.Range
		PathFilter  goatcounter.PathFilter
		AllowGroups goatcounter.Groups
		Widgets     widgets.List
		HLPeriod    string
		Group       goatcounter.Group
		Filter      string
		Total       int
		TotalUTC    int
	}{newGlobals(r), cd, showRefs, rng,
		args.PathFilter, allowGroups, wid, period, group, filter,
		shared.Total, shared.TotalUTC})
}

// loadDashboardWidgets owns the query goroutines for this request. Cancellation
// reaches every query, and joining them prevents rendering partially written data.
func (h backend) loadDashboardWidgets(r *http.Request, list widgets.List, args widgets.Args) error {
	if err := r.Context().Err(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, widget := range list {
		wg.Go(func() {
			defer func() {
				if p := recover(); p != nil {
					slog.ErrorContext(r.Context(), "widget panic", "panic", p, "stack", string(debug.Stack()), "widget", widget.Name(), requestAttrs(r))
					_, userErr := httpx.UserError(fmt.Errorf("widget panic: %v", p))
					widget.SetErr(userErr)
				}
			}()
			ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.dashTimeout)*time.Second)
			defer cancel()
			start := time.Now()
			_, err := widget.GetData(ctx, args)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					err = httpx.Error(http.StatusGatewayTimeout, "server timed out loading data")
				} else if !errors.Is(err, context.Canceled) {
					slog.With("module", "dashboard").ErrorContext(ctx, err.Error(), "widget", widget.Name(), requestAttrs(r))
					_, err = httpx.UserError(err)
				}
				widget.SetErr(err)
			}
			slog.With("module", "dashboard").DebugContext(r.Context(), widget.Name(), "took", time.Since(start))
		})
	}
	wg.Wait()
	return r.Context().Err()
}

func (h backend) loadWidget(w http.ResponseWriter, r *http.Request) error {
	rng, err := getPeriod(r)
	if err != nil {
		return err
	}

	v := validation.New()
	var (
		widget     = int(v.Integer("widget", r.URL.Query().Get("widget")))
		key        = r.URL.Query().Get("key")
		total      = int(v.Integer("total", r.URL.Query().Get("total")))
		offset     = int(v.Integer("offset", r.URL.Query().Get("offset")))
		pathFilter = getPathFilter(&v, r)
	)
	if v.HasErrors() {
		return v
	}

	args := widgets.SharedData{
		Site:     Site(r.Context()),
		TotalUTC: total,
		Total:    total,
		RowsOnly: key != "" || offset > 0,
		Args: widgets.Args{
			Rng:        rng,
			PathFilter: pathFilter,
			Offset:     offset,
		},
	}

	wid := widgets.ByID(r.Context(), widget)
	if wid == nil {
		return httpx.Errorf(400, `"widget" query parameter out of range: %d`, widget)
	}
	if key != "" {
		wid.SetDetail(key)
	}

	ret := make(map[string]any)
	switch wid.Name() {
	case "pages":
		p := wid.(*widgets.Pages)

		args.RowsOnly = true
		args.Args.Group, args.Args.AllowGroups = getGroup(r, args.Args.Group, rng)

		if key == "" {
			p.Max, err = strconv.Atoi(r.URL.Query().Get("max"))
			if err != nil {
				return httpx.Errorf(400, `"max" query parameter wrong: %w`, err)
			}
			p.Exclude, err = parse.Ints[goatcounter.PathID](r.URL.Query().Get("exclude"), ",")
			if err != nil {
				return httpx.Errorf(400, `"exclude" query parameter wrong: %w`, err)
			}
		}
	}

	ret["more"], err = wid.GetData(r.Context(), args.Args)
	if err != nil {
		return err
	}
	ret["html"], err = renderTemplate(wid.RenderHTML(r.Context(), args))
	if err != nil {
		return err
	}
	switch wid.Name() {
	case "pages":
		p := wid.(*widgets.Pages)
		ret["total_display"] = p.Display
		ret["max"] = p.Max
	}

	return httpx.JSON(w, ret)
}

// Get a time range; the return value is always in UTC, and is the UTC day range
// corresponding to the given timezone.
//
// So, for example a week in +08:00 would be:
// 2020-12-20 16:00:00 - 2020-12-27 15:59:59
//
// Values for rng:
//
//	week, month, quarter, half-year, year
//	   The start date is set to exactly this period ago. The end date is set to
//	   the end of the current day.
//
//	Any digit
//	   Last n days.
func timeRange(ctx context.Context, r string, tz *time.Location, sundayStartsWeek bool) datetime.Range {
	rng := datetime.NewRange(datetime.Now(ctx).In(tz)).Current(datetime.Day)
	switch r {
	case "0", "day":
	case "week":
		rng = rng.Last(datetime.Week(sundayStartsWeek))
	case "month":
		rng = rng.Last(datetime.Month)
	case "quarter":
		rng = rng.Last(datetime.Quarter)
	case "half-year":
		rng = rng.Last(datetime.HalfYear)
	case "year":
		rng = rng.Last(datetime.Year)
	default:
		// This can be a fraction such as "54.958333333333336" for views that
		// were saved from dashboard.js before it rounded the number: it
		// divided the difference of two dates at local midnight by 24 hours,
		// and with a DST transition in the range that's not a whole number.
		// Keep rounding here for views that were saved like that.
		days, err := strconv.ParseFloat(r, 32)
		if err != nil {
			slog.ErrorContext(ctx, fmt.Errorf("timeRange: %w", err).Error(), "rng", r)
			return timeRange(ctx, "week", tz, sundayStartsWeek)
		}
		rng.Start = datetime.AddPeriod(rng.Start, -int(math.Round(days)), datetime.Day)
	}
	return rng.UTC()
}

func getPeriod(r *http.Request) (datetime.Range, error) {
	var rng datetime.Range

	if d := r.URL.Query().Get("period-start"); d != "" {
		var err error
		rng.Start, err = time.ParseInLocation("2006-01-02", d, goatcounter.Config(r.Context()).Timezone.Loc())
		if err != nil {
			return rng, httpx.Error(400, "Invalid start date: "+d)
		}
	}
	if d := r.URL.Query().Get("period-end"); d != "" {
		var err error
		rng.End, err = time.ParseInLocation("2006-01-02 15:04:05", d+" 23:59:59", goatcounter.Config(r.Context()).Timezone.Loc())
		if err != nil {
			return rng, httpx.Error(400, "Invalid end date: "+d)
		}
	}

	if r.URL.Query().Get("period-start") == "" || r.URL.Query().Get("period-end") == "" {
		return timeRange(r.Context(), defaultPeriod, goatcounter.Config(r.Context()).Timezone.Loc(), false), nil
	}
	if rng.End.Before(rng.Start) {
		return rng, httpx.Error(400, "end date is before start date")
	}

	return rng.From(rng.Start).To(rng.End).UTC(), nil
}

func getGroup(r *http.Request, g goatcounter.Group, rng datetime.Range) (goatcounter.Group, goatcounter.Groups) {
	var (
		allow goatcounter.Groups
		saved = g
		d     = rng.End.Sub(rng.Start).Hours() / 24
	)
	// Viewing by hour for a year or viewing by day for 2 days looks horrible,
	// so don't allow that sort of thing.
	//
	// These numbers are based on what makes sense when you click "Last day ·
	// week · month · quarter · half year · year · all", which is probably what
	// most people use.
	switch {
	case d <= 6:
		g, allow = goatcounter.GroupHourly, append(allow, goatcounter.GroupHourly)
	case d >= 364:
		g, allow = goatcounter.GroupDaily, append(allow, goatcounter.GroupDaily, goatcounter.GroupWeekly, goatcounter.GroupMonthly)
	case d < 90:
		g, allow = goatcounter.GroupHourly, append(allow, goatcounter.GroupHourly, goatcounter.GroupDaily)
	case d >= 90:
		g, allow = goatcounter.GroupDaily, append(allow, goatcounter.GroupDaily, goatcounter.GroupWeekly)
	}
	// Keep the full date range, but avoid rendering millions of empty points.
	minimum := goatcounter.ChartGroup(rng, g)
	allow = slices.DeleteFunc(allow, func(group goatcounter.Group) bool { return group < minimum })
	if len(allow) == 0 {
		allow = goatcounter.Groups{minimum}
	}
	g = minimum

	// Keep the grouping from the saved view if it makes sense for this period,
	// instead of always resetting it to the default.
	if slices.Contains(allow, saved) {
		g = saved
	}

	switch gg := strings.ToLower(r.URL.Query().Get("group")); {
	case gg == "hour" && slices.Contains(allow, goatcounter.GroupHourly):
		g = goatcounter.GroupHourly
	case gg == "day" && slices.Contains(allow, goatcounter.GroupDaily):
		g = goatcounter.GroupDaily
	case gg == "week" && slices.Contains(allow, goatcounter.GroupWeekly):
		g = goatcounter.GroupWeekly
	case gg == "month" && slices.Contains(allow, goatcounter.GroupMonthly):
		g = goatcounter.GroupMonthly
	case gg == "year" && slices.Contains(allow, goatcounter.GroupYearly):
		g = goatcounter.GroupYearly
	}

	return g, allow
}

func getPathFilter(v *validation.Validator, r *http.Request) goatcounter.PathFilter {
	f := r.URL.Query().Get("filter")
	if f == "" {
		return goatcounter.PathFilter{}
	}

	filter, err := goatcounter.PathFilterFromQuery(r.Context(), f)
	if err != nil {
		v.Append("filter", err.Error())
	}
	return filter
}
