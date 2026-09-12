package widgets

import (
	"context"
	"html/template"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/log"
	"zgo.at/zstd/zint"
	"zgo.at/zstd/ztime"
)

type (
	Widget interface {
		GetData(context.Context, Args) (bool, error)
		RenderHTML(context.Context, SharedData) (string, any)

		SetHTML(template.HTML)
		HTML() template.HTML
		SetErr(error)
		Err() error

		// SetDetail sets the drill-down key: the browser/system/country/… to
		// show the details for, or the path to show referrers for.
		SetDetail(string)

		ID() int

		Name() string
		Type() string // "full-width", "hchart"
		Label(context.Context) string
	}

	Args struct {
		Rng         ztime.Range
		Offset      int
		PathFilter  goatcounter.PathFilter
		Group       goatcounter.Group
		AllowGroups goatcounter.Groups
		ShowRefs    goatcounter.PathID
	}

	// SharedData gets passed to every widget.
	SharedData struct {
		Site *goatcounter.Site
		User *goatcounter.User
		Args Args

		RowsOnly    bool
		Total       int
		TotalUTC    int
		TotalEvents int
	}
)

type List []Widget

func NewArgs(
	ctx context.Context,
	rng ztime.Range, group goatcounter.Group, allowGroups goatcounter.Groups,
	showRefs goatcounter.PathID,
) Args {

	// Align to start of week or month if we're grouping by week or month.
	//
	// This gives a really jarring experience if the UI is updated with the new
	// dates, as switching between day/week/month can really move the date
	// around. So don't update the UI and just "silently" include the extra date
	// ranges.
	if group.Weekly() {
		w := ztime.Week(false)
		rng.Start = ztime.StartOf(rng.Start.In(goatcounter.Config(ctx).Timezone.Loc()), w).UTC()
		rng.End = ztime.EndOf(rng.End.In(goatcounter.Config(ctx).Timezone.Loc()), w).UTC()
	}
	if group.Monthly() {
		rng.Start = ztime.StartOf(rng.Start.In(goatcounter.Config(ctx).Timezone.Loc()), ztime.Month).UTC()
		rng.End = ztime.EndOf(rng.End.In(goatcounter.Config(ctx).Timezone.Loc()), ztime.Month).UTC()
	}

	return Args{Rng: rng, Group: group, AllowGroups: allowGroups, ShowRefs: showRefs}
}

// Layout is the dashboard layout: the widgets that are shown, in order. It is
// not configurable.
var Layout = []string{
	"totalpages",
	"pages",
	"toprefs",
	"campaigns",
	"browsers",
	"systems",
	"locations",
	"languages",
	"sizes",
}

// NewList creates the widgets for the dashboard, in the order they're shown.
//
// The "totalcount" widget is always first: it's not rendered itself, but every
// other widget needs its totals.
func NewList(ctx context.Context) List {
	l := make(List, 0, len(Layout)+1)
	l = append(l, NewWidget(ctx, "totalcount", 0))
	for i, name := range Layout {
		l = append(l, NewWidget(ctx, name, i))
	}
	return l
}

// ByID gets the widget at this position in the layout.
func ByID(ctx context.Context, id int) Widget {
	if id < 0 || id >= len(Layout) {
		return &Dummy{}
	}
	return NewWidget(ctx, Layout[id], id)
}

// GetOne gets the first widget in the list by name.
//
// You usually want to use Get()! Only intended to get "internal" widgets where
// you know it will always have exactly one in the list.
func (l List) GetOne(name string) Widget {
	for _, w := range l {
		if w.Name() == name {
			return w
		}
	}
	return nil
}

// Get all widgets from the list by name.
func (l List) Get(name string) List {
	list := make([]Widget, 0, 1)
	for _, w := range l {
		if w.Name() == name {
			list = append(list, w)
		}
	}
	return list
}

// How many rows every widget shows before you need to press "show more".
const (
	pageSize    = 10 // Paths overview.
	refPageSize = 10 // Referrers for one path.
	hchartSize  = 6  // Browsers, systems, locations, …
)

func NewWidget(ctx context.Context, name string, id int) Widget {
	switch name {
	case "totalcount":
		return &TotalCount{}

	case "pages":
		return &Pages{id: id, Limit: pageSize, LimitRefs: refPageSize, Style: "line"}
	case "totalpages":
		return &TotalPages{id: id, Style: "line"}
	case "toprefs":
		return &TopRefs{id: id, Limit: hchartSize}
	case "campaigns":
		return &Campaigns{id: id, Limit: hchartSize}
	case "browsers":
		return &Browsers{id: id, Limit: hchartSize}
	case "systems":
		return &Systems{id: id, Limit: hchartSize}
	case "sizes":
		return &Sizes{id: id}
	case "locations":
		return &Locations{id: id, Limit: hchartSize}
	case "languages":
		return &Languages{id: id, Limit: hchartSize}
	}
	log.Errorf(ctx, "unknown widget: %q", name)
	return &Dummy{}
}

func isCol(ctx context.Context, flag zint.Bitflag16) bool {
	return goatcounter.MustGetSite(ctx).Settings.Collect.Has(flag)
}
