package widgets

import (
	"context"
	"html/template"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/i18n"
)

type Systems struct {
	id     int
	loaded bool
	err    error
	html   template.HTML

	Limit  int
	Detail string
	Stats  goatcounter.HitStats
}

func (w Systems) Name() string { return "systems" }
func (w Systems) Type() string { return "hchart" }
func (w Systems) Label(ctx context.Context) string {
	return i18n.T(ctx, "label/system-stats|System stats")
}
func (w *Systems) SetHTML(h template.HTML) { w.html = h }
func (w Systems) HTML() template.HTML      { return w.html }
func (w *Systems) SetErr(h error)          { w.err = h }
func (w Systems) Err() error               { return w.err }
func (w Systems) ID() int                  { return w.id }

func (w *Systems) SetDetail(d string) { w.Detail = d }

func (w *Systems) GetData(ctx context.Context, a Args) (more bool, err error) {
	if w.Detail != "" {
		err = w.Stats.ListSystem(ctx, w.Detail, a.Rng, a.PathFilter, w.Limit, a.Offset)
	} else {
		err = w.Stats.ListSystems(ctx, a.Rng, a.PathFilter, w.Limit, a.Offset)
	}
	w.loaded = true
	return w.Stats.More, err
}

func (w Systems) RenderHTML(ctx context.Context, shared SharedData) (string, any) {
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
		isCol(ctx, goatcounter.CollectUserAgent), i18n.T(ctx, "header/systems|Systems"),
		shared.TotalUTC, w.Stats, w.Detail}
}
