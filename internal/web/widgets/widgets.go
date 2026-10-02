package widgets

import (
	"context"
	"html/template"

	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/datetime"
)

type Widget interface {
	Name() string

	// GetData loads the data from the database, and reports if there are
	// more rows to paginate.
	GetData(context.Context, Args) (more bool, err error)

	// RenderHTML returns the template name and the data to render it with.
	RenderHTML(context.Context, Args) (string, any)

	// SetDetail sets the drill-down key: the browser/system/country/… to
	// show the details for, or the path to show referrers for.
	SetDetail(string)

	SetHTML(template.HTML)
	HTML() template.HTML
	SetErr(error)
	Err() error
}

// Args are passed to every widget.
type Args struct {
	Rng        datetime.Range
	PathFilter analytics.PathFilter
	Group      analytics.Group
	ShowRefs   string // Show the referrers for this path in the pages list.
	Offset     int

	// For rendering.
	Total    int  // Number of visits; the percentages are relative to this.
	RowsOnly bool // Only render the rows, for pagination.
}

// NewArgs creates the arguments for a period and grouping.
func NewArgs(ctx context.Context, rng datetime.Range, group analytics.Group, filter analytics.PathFilter) Args {
	// Align to start of week or month if we're grouping by week or month.
	//
	// This gives a really jarring experience if the UI is updated with the new
	// dates, as switching between day/week/month can really move the date
	// around. So don't update the UI and just "silently" include the extra date
	// ranges.
	align := func(p datetime.Period) {
		loc := analytics.Config(ctx).Timezone.Loc()
		rng.Start = datetime.StartOf(rng.Start.In(loc), p).UTC()
		rng.End = datetime.EndOf(rng.End.In(loc), p).UTC()
	}
	switch group {
	case analytics.GroupWeekly:
		align(datetime.Week(false))
	case analytics.GroupMonthly:
		align(datetime.Month)
	}
	return Args{Rng: rng, Group: group, PathFilter: filter}
}

// base implements the parts of Widget that are the same for every widget.
type base struct {
	name string
	err  error
	html template.HTML
}

func (w base) Name() string             { return w.name }
func (w *base) SetHTML(h template.HTML) { w.html = h }
func (w base) HTML() template.HTML      { return w.html }
func (w *base) SetErr(err error)        { w.err = err }
func (w base) Err() error               { return w.err }

// How many rows every widget shows before you need to press "show more".
const (
	pageSize    = 6  // Paths overview.
	refPageSize = 10 // Referrers for one path.
	hchartSize  = 6  // Browsers, systems, locations, …
)

type (
	// Card is a dashboard card, which shows one of its tabs at a time.
	Card struct {
		Name, Label string
		Tabs        []Tab
	}
	Tab struct {
		Widget, Label string
	}
)

// Cards is the dashboard layout below the totals; it is not configurable.
var Cards = []Card{
	{"content", "Content", []Tab{
		{"pages", "Pages"},
		{"entry_pages", "Entry pages"},
		{"exit_pages", "Exit pages"},
		{"events", "Events"},
	}},
	{"acquisition", "Acquisition", []Tab{
		{"toprefs", "Sources"},
		{"campaigns", "Campaigns"},
		{"utm_mediums", "UTM mediums"},
		{"utm_sources", "UTM sources"},
	}},
	{"technology", "Technology", []Tab{
		{"browsers", "Browsers"},
		{"systems", "Operating systems"},
		{"sizes", "Devices"},
	}},
	{"audience", "Audience", []Tab{
		{"locations", "Locations"},
		{"languages", "Languages"},
	}},
}

// New creates a widget by name, or returns nil if there is no such widget.
func New(name string) Widget {
	switch name {
	case "totals":
		return &Totals{base: base{name: name}}
	case "pages":
		return &Pages{base: base{name: name}}

	// Breakdowns with a detail view.
	case "browsers", "systems", "sizes", "campaigns":
		return &Breakdown{base: base{name: name}, detailKind: name}
	case "toprefs":
		return &Breakdown{base: base{name: name}, detailKind: "refpaths"}

	case "languages", "entry_pages", "exit_pages", "events", "utm_mediums", "utm_sources", "locations":
		return &Breakdown{base: base{name: name}}
	}
	return nil
}

type List []Widget

// NewList creates all widgets on the dashboard.
func NewList() List {
	l := List{New("totals")}
	for _, c := range Cards {
		for _, t := range c.Tabs {
			l = append(l, New(t.Widget))
		}
	}
	return l
}

// Get a widget by name, or nil if it's not in the list.
func (l List) Get(name string) Widget {
	for _, w := range l {
		if w.Name() == name {
			return w
		}
	}
	return nil
}

// HTML gets the rendered HTML of a widget.
func (l List) HTML(name string) template.HTML {
	if w := l.Get(name); w != nil {
		return w.HTML()
	}
	return ""
}
