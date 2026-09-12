package widgets

import (
	"context"
	"html/template"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/i18n"
	"zgo.at/zstd/ztime"
)

type TotalPages struct {
	id     int
	loaded bool
	err    error
	html   template.HTML

	Align, NoEvents bool
	Style           string
	Total           goatcounter.HitList
}

func (w TotalPages) Name() string { return "totalpages" }
func (w TotalPages) Type() string { return "full-width" }
func (w TotalPages) Label(ctx context.Context) string {
	return i18n.T(ctx, "label/total-pageviews|Total site pageviews")
}
func (w *TotalPages) SetHTML(h template.HTML) { w.html = h }
func (w TotalPages) HTML() template.HTML      { return w.html }
func (w *TotalPages) SetErr(h error)          { w.err = h }
func (w TotalPages) Err() error               { return w.err }
func (w TotalPages) ID() int                  { return w.id }

func (w *TotalPages) SetDetail(d string) {}

func (w *TotalPages) GetData(ctx context.Context, a Args) (more bool, err error) {
	err = w.Total.Totals(ctx, a.Rng, a.PathFilter, a.Group, w.NoEvents)
	w.loaded = true
	return false, err
}

func (w TotalPages) RenderHTML(ctx context.Context, shared SharedData) (string, any) {
	// Set days in the future to -1; we filter this in the JS when rendering the
	// chart. It's easier to do this here because JavaScript Date() has
	// piss-poor support for timezones.
	//
	// Only remove them if the last day is today: for everything else we want to
	// display the future as "greyed out".
	var (
		now   = ztime.Now(ctx).In(goatcounter.Config(ctx).Timezone.Loc())
		today = now.Format("2006-01-02")
		hour  = now.Hour()
	)
	if len(w.Total.Stats) > 0 && w.Total.Stats[len(w.Total.Stats)-1].Day == today {
		j := len(w.Total.Stats) - 1
		w.Total.Stats[j].Hourly = w.Total.Stats[j].Hourly[:hour+1]
	}

	return "_dashboard_totals.gohtml", struct {
		Context context.Context
		Site    *goatcounter.Site
		User    *goatcounter.User
		ID      int
		Loaded  bool
		Err     error

		Align    bool
		NoEvents bool
		Page     goatcounter.HitList
		Group    goatcounter.Group
		Max      int

		Total       int
		TotalEvents int

		Style string
	}{ctx, shared.Site, shared.User, w.id, w.loaded, w.err,
		w.Align, w.NoEvents,
		w.Total, shared.Args.Group, w.Total.Max,
		shared.Total, shared.TotalEvents,
		w.Style}
}
