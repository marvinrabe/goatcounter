package widgets

import (
	"context"

	"github.com/marvinrabe/goatcounter"
)

// Totals shows the metrics for the period, and a chart of one of them.
type Totals struct {
	base
	Data goatcounter.DashboardData
}

func (w *Totals) SetDetail(string) {}

// Visits in the period; the other widgets show percentages of this.
func (w Totals) Visits() int { return w.Data.Metrics.Visits }

func (w *Totals) GetData(ctx context.Context, a Args) (bool, error) {
	var err error
	w.Data, err = goatcounter.GetDashboardData(ctx, a.Rng, a.PathFilter, a.Group)
	return false, err
}

func (w Totals) RenderHTML(ctx context.Context, a Args) (string, any) {
	return "_dashboard_totals.gohtml", struct {
		Name    string
		Err     error
		Metrics goatcounter.DashboardMetrics
		Prev    goatcounter.DashboardMetrics
		Series  goatcounter.DashboardMetricSeries
	}{w.name, w.err, w.Data.Metrics, w.Data.Prev, w.Data.Series}
}
