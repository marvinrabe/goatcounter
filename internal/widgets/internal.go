package widgets

import (
	"context"
	"html/template"

	"github.com/marvinrabe/goatcounter"
)

type TotalCount struct {
	goatcounter.TotalCount
	Metrics goatcounter.DashboardMetrics

	loaded bool
	err    error
	html   template.HTML

	NoEvents bool
}

func (w TotalCount) Name() string                                         { return "totalcount" }
func (w TotalCount) Type() string                                         { return "data-only" }
func (w TotalCount) Label() string                                        { return "" }
func (w *TotalCount) SetHTML(h template.HTML)                             {}
func (w TotalCount) HTML() template.HTML                                  { return w.html }
func (w *TotalCount) SetErr(h error)                                      { w.err = h }
func (w TotalCount) Err() error                                           { return w.err }
func (w TotalCount) ID() int                                              { return 0 }
func (w *TotalCount) SetDetail(d string)                                  {}
func (w TotalCount) RenderHTML(context.Context, SharedData) (string, any) { return "", nil }

func (w *TotalCount) GetData(ctx context.Context, a Args) (more bool, err error) {
	w.TotalCount, err = goatcounter.GetTotalCount(ctx, a.Rng, a.PathFilter, w.NoEvents)
	if err != nil {
		return false, err
	}
	data, err := a.dashboardData(ctx)
	w.Metrics = data.Metrics
	w.loaded = true
	return false, err
}
