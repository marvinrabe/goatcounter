package widgets

import (
	"context"

	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/datetime"
)

// Breakdown shows the visits by one dimension, such as browsers or entry
// pages. The name is the breakdown kind for ListVisitorBreakdown.
type Breakdown struct {
	base

	// Breakdown kind for the detail of a row; empty if rows have no detail.
	detailKind string

	Detail        string
	Stats         analytics.HitStats
	MostlyUnknown bool // Location lookups don't seem to work.
}

func (w *Breakdown) SetDetail(d string) {
	if w.detailKind != "" {
		w.Detail = d
	}
}

func (w *Breakdown) GetData(ctx context.Context, a Args) (bool, error) {
	var err error
	switch {
	case w.Detail != "":
		err = w.Stats.ListVisitorBreakdown(ctx, w.detailKind, w.Detail, a.Rng, a.PathFilter, hchartSize, a.Offset)
	case w.name == "sizes":
		err = w.Stats.ListVisitorSizes(ctx, a.Rng, a.PathFilter, false)
	default:
		err = w.Stats.ListVisitorBreakdown(ctx, w.name, "", a.Rng, a.PathFilter, hchartSize, a.Offset)
	}
	if w.name == "locations" && w.Detail == "" {
		w.MostlyUnknown = len(w.Stats.Stats) > 0 && w.Stats.Stats[0].ID == "" &&
			datetime.StartOf(a.Rng.End, datetime.Day).Equal(datetime.StartOf(datetime.Now(ctx), datetime.Day))
	}
	return w.Stats.More, err
}

func (w Breakdown) RenderHTML(ctx context.Context, a Args) (string, any) {
	unnamed := nameUnknown
	if w.name == "toprefs" {
		unnamed = nameDirect
	}
	chart := newChart(w.Stats, a.Total, w.detailKind != "" && w.Detail == "", unnamed, a.RowsOnly)

	// A detail, or more rows.
	if w.Detail != "" || a.RowsOnly {
		return "_chart.gohtml", chart
	}
	return "_dashboard_hchart.gohtml", struct {
		Name          string
		Err           error
		Chart         Chart
		MostlyUnknown bool
	}{w.name, w.err, chart, w.MostlyUnknown}
}
