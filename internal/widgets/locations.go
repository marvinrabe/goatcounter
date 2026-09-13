package widgets

import (
	"context"
	"html/template"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/datetime"
)

type Locations struct {
	id     int
	loaded bool
	err    error
	html   template.HTML

	Limit         int
	Detail        string
	Stats         goatcounter.HitStats
	MostlyUnknown bool
}

func (w Locations) Name() string { return "locations" }
func (w Locations) Type() string { return "hchart" }
func (w Locations) Label() string {
	return "Location stats"
}
func (w *Locations) SetHTML(h template.HTML) { w.html = h }
func (w Locations) HTML() template.HTML      { return w.html }
func (w *Locations) SetErr(h error)          { w.err = h }
func (w Locations) Err() error               { return w.err }
func (w Locations) ID() int                  { return w.id }

func (w *Locations) SetDetail(d string) { w.Detail = d }

func (w *Locations) GetData(ctx context.Context, a Args) (more bool, err error) {
	if w.Detail != "" {
		err = w.Stats.ListLocation(ctx, w.Detail, a.Rng, a.PathFilter, w.Limit, a.Offset)
	} else {
		err = w.Stats.ListLocations(ctx, a.Rng, a.PathFilter, w.Limit, a.Offset)
		w.MostlyUnknown = len(w.Stats.Stats) > 0 && w.Stats.Stats[0].ID == "" &&
			datetime.StartOf(a.Rng.End, datetime.Day).Equal(datetime.StartOf(datetime.Now(ctx), datetime.Day))
	}
	w.loaded = true
	return w.Stats.More, err
}

func (w Locations) RenderHTML(ctx context.Context, shared SharedData) (string, any) {
	header := "Locations"
	if w.err == nil && w.Detail != "" {
		var l goatcounter.Location
		err := l.ByCode(ctx, w.Detail)
		if err != nil {
			w.err = err
		}
		header = "Locations for " + l.CountryName
	}

	return "_dashboard_hchart.gohtml", struct {
		Context       context.Context
		Name          string
		ID            int
		RowsOnly      bool
		HasSubMenu    bool
		Loaded        bool
		Err           error
		Header        string
		TotalUTC      int
		Stats         goatcounter.HitStats
		Detail        string
		MostlyUnknown bool
	}{ctx, w.Name(), w.id, shared.RowsOnly, w.Detail == "", w.loaded, w.err,
		header, shared.TotalUTC, w.Stats, w.Detail, w.MostlyUnknown}
}
