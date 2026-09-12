package widgets

import (
	"context"
	"html/template"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/i18n"
)

type Browsers struct {
	id     int
	loaded bool
	err    error
	html   template.HTML

	Limit  int
	Detail string
	Stats  goatcounter.HitStats
}

func (w Browsers) Name() string { return "browsers" }
func (w Browsers) Type() string { return "hchart" }
func (w Browsers) Label(ctx context.Context) string {
	return i18n.T(ctx, "label/browser-stats|Browser stats")
}
func (w *Browsers) SetHTML(h template.HTML) { w.html = h }
func (w Browsers) HTML() template.HTML      { return w.html }
func (w *Browsers) SetErr(h error)          { w.err = h }
func (w Browsers) Err() error               { return w.err }
func (w Browsers) ID() int                  { return w.id }

func (w *Browsers) SetDetail(d string) { w.Detail = d }

func (w *Browsers) GetData(ctx context.Context, a Args) (more bool, err error) {
	if w.Detail != "" {
		err = w.Stats.ListBrowser(ctx, w.Detail, a.Rng, a.PathFilter, w.Limit, a.Offset)
	} else {
		err = w.Stats.ListBrowsers(ctx, a.Rng, a.PathFilter, w.Limit, a.Offset)
	}
	w.loaded = true
	return w.Stats.More, err
}

func (w Browsers) RenderHTML(ctx context.Context, shared SharedData) (string, any) {
	return "_dashboard_hchart.gohtml", struct {
		Context     context.Context
		Base        string
		Name        string
		ID          int
		RowsOnly    bool
		HasSubMenu  bool
		Loaded      bool
		Err         error
		IsCollected bool
		Header      string
		TotalUTC    int
		Stats       goatcounter.HitStats
		Detail      string
	}{ctx, goatcounter.Config(ctx).BasePath, w.Name(), w.id, shared.RowsOnly, w.Detail == "", w.loaded, w.err,
		isCol(ctx, goatcounter.CollectUserAgent), i18n.T(ctx, "header/browsers|Browsers"),
		shared.TotalUTC, w.Stats, w.Detail}
}
