package widgets

import (
	"context"
	"html/template"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/i18n"
)

type Languages struct {
	id     int
	loaded bool
	err    error
	html   template.HTML

	Limit int
	Stats goatcounter.HitStats
}

func (w Languages) Name() string { return "languages" }
func (w Languages) Type() string { return "hchart" }
func (w Languages) Label(ctx context.Context) string {
	return i18n.T(ctx, "label/language-stats|Language stats")
}
func (w *Languages) SetHTML(h template.HTML) { w.html = h }
func (w Languages) HTML() template.HTML      { return w.html }
func (w *Languages) SetErr(h error)          { w.err = h }
func (w Languages) Err() error               { return w.err }
func (w Languages) ID() int                  { return w.id }

// Languages have no detail view; only the language itself is stored, not the
// region.
func (w *Languages) SetDetail(string) {}

func (w *Languages) GetData(ctx context.Context, a Args) (more bool, err error) {
	err = w.Stats.ListLanguages(ctx, a.Rng, a.PathFilter, w.Limit, a.Offset)
	w.loaded = true
	return w.Stats.More, err
}

func (w Languages) RenderHTML(ctx context.Context, shared SharedData) (string, any) {
	return "_dashboard_hchart.gohtml", struct {
		Context    context.Context
		Name       string
		ID         int
		RowsOnly   bool
		HasSubMenu bool
		Loaded     bool
		Err        error
		Header     string
		TotalUTC   int
		Stats      goatcounter.HitStats
	}{ctx, w.Name(), w.id, shared.RowsOnly, false, w.loaded, w.err,
		i18n.T(ctx, "header/languages|Languages"),
		shared.TotalUTC, w.Stats}
}
