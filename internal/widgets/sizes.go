package widgets

import (
	"context"
	"html/template"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/i18n"
)

type Sizes struct {
	id     int
	loaded bool
	err    error
	html   template.HTML

	Limit       int
	Detail      string
	SortByCount bool
	Stats       goatcounter.HitStats
}

func (w Sizes) Name() string { return "sizes" }
func (w Sizes) Type() string { return "hchart" }
func (w Sizes) Label(ctx context.Context) string {
	return i18n.T(ctx, "label/size-stats|Size stats")
}
func (w *Sizes) SetHTML(h template.HTML) { w.html = h }
func (w Sizes) HTML() template.HTML      { return w.html }
func (w *Sizes) SetErr(h error)          { w.err = h }
func (w Sizes) Err() error               { return w.err }
func (w Sizes) ID() int                  { return w.id }

func (w *Sizes) SetDetail(d string) { w.Detail = d }

func (w *Sizes) GetData(ctx context.Context, a Args) (more bool, err error) {
	if w.Detail != "" {
		err = w.Stats.ListSize(ctx, w.Detail, a.Rng, a.PathFilter, 6, a.Offset)
	} else {
		err = w.Stats.ListSizes(ctx, a.Rng, a.PathFilter, w.SortByCount)
	}
	w.loaded = true
	return w.Stats.More, err
}

func (w Sizes) RenderHTML(ctx context.Context, shared SharedData) (string, any) {
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
		isCol(ctx, goatcounter.CollectScreenSize), i18n.T(ctx, "header/sizes|Sizes"),
		shared.TotalUTC, w.Stats, w.Detail}
}
