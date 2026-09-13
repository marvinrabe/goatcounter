package widgets

import (
	"context"
	"html/template"

	"github.com/marvinrabe/goatcounter"
)

type TotalPages struct {
	id     int
	loaded bool
	err    error
	html   template.HTML

	// Total remains part of the dashboard API response.
	Total  goatcounter.HitList
	Series goatcounter.DashboardMetricSeries
}

func (w TotalPages) Name() string { return "totalpages" }
func (w TotalPages) Type() string { return "full-width" }
func (w TotalPages) Label() string {
	return "Total site pageviews"
}
func (w *TotalPages) SetHTML(h template.HTML) { w.html = h }
func (w TotalPages) HTML() template.HTML      { return w.html }
func (w *TotalPages) SetErr(h error)          { w.err = h }
func (w TotalPages) Err() error               { return w.err }
func (w TotalPages) ID() int                  { return w.id }

func (w *TotalPages) SetDetail(d string) {}

func (w *TotalPages) GetData(ctx context.Context, a Args) (more bool, err error) {
	data, err := a.dashboardData(ctx)
	w.Series = data.Series
	w.loaded = true
	return false, err
}

func (w TotalPages) RenderHTML(ctx context.Context, shared SharedData) (string, any) {
	return "_dashboard_totals.gohtml", struct {
		Context context.Context
		ID      int
		Loaded  bool
		Err     error

		Group   goatcounter.Group
		Metrics goatcounter.DashboardMetrics
		Series  goatcounter.DashboardMetricSeries
	}{ctx, w.id, w.loaded, w.err, shared.Args.Group, shared.Metrics, w.Series}
}
