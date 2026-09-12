package widgets

import (
	"context"
	"html/template"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/i18n"
)

type TopRefs struct {
	id     int
	loaded bool
	err    error
	html   template.HTML

	Limit   int
	Ref     string
	TopRefs goatcounter.HitStats
}

func (w TopRefs) Name() string { return "toprefs" }
func (w TopRefs) Type() string { return "hchart" }
func (w TopRefs) Label(ctx context.Context) string {
	return i18n.T(ctx, "label/topref|Top referrals")
}
func (w *TopRefs) SetHTML(h template.HTML) { w.html = h }
func (w TopRefs) HTML() template.HTML      { return w.html }
func (w *TopRefs) SetErr(h error)          { w.err = h }
func (w TopRefs) Err() error               { return w.err }
func (w TopRefs) ID() int                  { return w.id }

func (w *TopRefs) SetDetail(d string) { w.Ref = d }

func (w *TopRefs) GetData(ctx context.Context, a Args) (more bool, err error) {
	if w.Ref != "" {
		err = w.TopRefs.ListTopRef(ctx, w.Ref, a.Rng, a.PathFilter, w.Limit, a.Offset)
	} else {
		err = w.TopRefs.ListTopRefs(ctx, a.Rng, a.PathFilter, w.Limit, a.Offset)
	}
	w.loaded = true
	return w.TopRefs.More, err
}

func (w TopRefs) RenderHTML(ctx context.Context, shared SharedData) (string, any) {
	return "_dashboard_toprefs.gohtml", struct {
		Context    context.Context
		Name       string
		ID         int
		RowsOnly   bool
		HasSubMenu bool
		Loaded     bool
		Err        error
		Total      int
		Stats      goatcounter.HitStats
		Ref        string
	}{ctx, w.Name(), w.id, shared.RowsOnly, w.Ref == "", w.loaded, w.err,
		shared.Total, w.TopRefs, w.Ref}
}
