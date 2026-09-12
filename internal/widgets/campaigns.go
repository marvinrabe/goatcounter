package widgets

import (
	"context"
	"html/template"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/i18n"
	"zgo.at/zstd/zstrconv"
)

type Campaigns struct {
	id     int
	loaded bool
	err    error
	html   template.HTML

	Limit    int
	Campaign goatcounter.CampaignID
	Stats    goatcounter.HitStats
}

func (w Campaigns) Name() string { return "campaigns" }
func (w Campaigns) Type() string { return "hchart" }
func (w Campaigns) Label(ctx context.Context) string {
	return i18n.T(ctx, "label/campaigns|Campaigns")
}
func (w *Campaigns) SetHTML(h template.HTML) { w.html = h }
func (w Campaigns) HTML() template.HTML      { return w.html }
func (w *Campaigns) SetErr(h error)          { w.err = h }
func (w Campaigns) Err() error               { return w.err }
func (w Campaigns) ID() int                  { return w.id }

func (w *Campaigns) SetDetail(d string) {
	w.Campaign, _ = zstrconv.ParseInt[goatcounter.CampaignID](d, 10)
}

func (w *Campaigns) GetData(ctx context.Context, a Args) (more bool, err error) {
	if w.Campaign > 0 {
		err = w.Stats.ListCampaign(ctx, w.Campaign, a.Rng, a.PathFilter, w.Limit, a.Offset)
	} else {
		err = w.Stats.ListCampaigns(ctx, a.Rng, a.PathFilter, w.Limit, a.Offset)
	}
	w.loaded = true
	return w.Stats.More, err
}

func (w Campaigns) RenderHTML(ctx context.Context, shared SharedData) (string, any) {
	//return "_dashboard_campaigns.gohtml", struct {
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
		Campaign   goatcounter.CampaignID
	}{ctx, w.Name(), w.id, shared.RowsOnly, w.Campaign == 0, w.loaded, w.err,
		w.Label(ctx),
		shared.TotalUTC, w.Stats, w.Campaign}
}
