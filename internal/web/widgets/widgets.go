package widgets

import (
	"context"

	"github.com/marvinrabe/goatcounter/internal/analytics"
)

// Kind is one of the breakdowns on the dashboard cards, such as browsers or
// entry pages. The name is the breakdown kind for Store.Breakdown.
type Kind struct {
	Name, Label string

	Pages   bool   // Rows are paths; the pages list.
	Detail  string // Breakdown kind for the detail of a row; empty if rows have no detail.
	Unnamed string // Name for rows without a name; "(unknown)" if empty.
}

// Panel is a loaded card tab.
type Panel struct {
	Kind
	Err   error
	Chart Chart
}

// Card is a dashboard card, which shows one of its tabs at a time.
type Card struct {
	Name, Label string
	Tabs        []Kind
}

// Cards is the dashboard layout below the totals; it is not configurable.
var Cards = []Card{
	{"content", "Content", []Kind{
		{Name: "pages", Label: "Pages", Pages: true, Detail: "pagerefs", Unnamed: nameDirect},
		{Name: "entry_pages", Label: "Entry pages"},
		{Name: "exit_pages", Label: "Exit pages"},
		{Name: "events", Label: "Events"},
	}},
	{"acquisition", "Acquisition", []Kind{
		{Name: "toprefs", Label: "Sources", Detail: "refpaths", Unnamed: nameDirect},
		{Name: "campaigns", Label: "Campaigns", Detail: "campaigns"},
		{Name: "utm_mediums", Label: "UTM mediums"},
		{Name: "utm_sources", Label: "UTM sources"},
	}},
	{"technology", "Technology", []Kind{
		{Name: "browsers", Label: "Browsers", Detail: "browsers"},
		{Name: "systems", Label: "Operating systems", Detail: "systems"},
		{Name: "sizes", Label: "Devices"},
	}},
	{"audience", "Audience", []Kind{
		{Name: "locations", Label: "Locations"},
		{Name: "languages", Label: "Languages"},
	}},
}

// Kinds are all the breakdowns on the cards.
func Kinds() []Kind {
	var l []Kind
	for _, c := range Cards {
		l = append(l, c.Tabs...)
	}
	return l
}

// Find a breakdown by name.
func Find(name string) (Kind, bool) {
	for _, k := range Kinds() {
		if k.Name == name {
			return k, true
		}
	}
	return Kind{}, false
}

// How many rows a chart shows before you need to press "show more".
const (
	pageSize    = 6
	refPageSize = 10 // Referrers for one path.
)

// Load gets the rows of the breakdown, or with key the rows of the detail of
// that row.
func (k Kind) Load(ctx context.Context, store *analytics.Store, q analytics.Query, key string, offset int) (analytics.Breakdown, error) {
	switch {
	case key != "" && k.Pages:
		return store.Breakdown(ctx, q, k.Detail, key, refPageSize, offset)
	case key != "":
		return store.Breakdown(ctx, q, k.Detail, key, pageSize, offset)
	case k.Pages:
		return store.Pages(ctx, q, pageSize, offset)
	case k.Name == "sizes":
		return store.Sizes(ctx, q, false)
	}
	return store.Breakdown(ctx, q, k.Name, "", pageSize, offset)
}
