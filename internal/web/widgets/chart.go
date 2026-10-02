package widgets

import (
	"cmp"
	"fmt"
	"math"
	"strings"

	"github.com/marvinrabe/goatcounter/internal/analytics"
)

// Chart is a horizontal bar chart; html/template owns all markup and escaping.
type Chart struct {
	Widget   string // Kind name, for loading more rows or a detail.
	Rows     []ChartRow
	Pages    bool // Rows are paths, which can show their referrers.
	RowsOnly bool // Render only the rows, without the pagination links.
	More     bool
}

type ChartRow struct {
	Key        string // Key to load the detail for; the path for pages.
	Name       string
	Class      string // Additional CSS classes.
	Percentage string
	VisitURL   string
	Count      int
	Page, Link bool
	Detail     *Chart // Shown below the row.
}

// Names for rows without a name.
const (
	nameUnknown = "(unknown)"
	nameDirect  = "Direct / none"
)

// Chart creates the chart for the rows of the breakdown, or with key for the
// detail of that row; the percentages are of total.
func (k Kind) Chart(b analytics.Breakdown, key string, total int) Chart {
	c := Chart{Widget: k.Name, More: b.More, Pages: k.Pages && key == ""}
	if total == 0 {
		return c
	}
	link := k.Detail != "" && key == ""
	unnamed := cmp.Or(k.Unnamed, nameUnknown)
	for _, s := range b.Rows {
		name := s.Name
		unknown := name == ""
		if unknown {
			name = unnamed
		}
		row := ChartRow{Key: s.ID, Count: s.Count, Percentage: percentage(s.Count, total),
			Page: c.Pages, Link: link && !unknown}
		if unknown || (s.RefScheme != nil && *s.RefScheme == analytics.RefSchemeGenerated) {
			row.Class = "generated"
		}
		if !link && s.RefScheme != nil && *s.RefScheme == analytics.RefSchemeHTTP {
			row.VisitURL = "http://" + name
		}
		if strings.HasPrefix(name, "twitter.com/search?q=") {
			if i := strings.LastIndex(name, "t.co%2F"); i > -1 {
				name = "Twitter link: t.co/" + name[i+7:]
			}
		}
		if row.Key == "" {
			row.Key = name
		}
		row.Name = elideCenter(name, 76)
		c.Rows = append(c.Rows, row)
	}
	return c
}

// Expand shows the detail below the row with key.
func (c *Chart) Expand(key string, detail func(count int) Chart) {
	for i := range c.Rows {
		if r := &c.Rows[i]; r.Key == key {
			d := detail(r.Count)
			r.Class, r.Detail = "target", &d
		}
	}
}

func percentage(count, total int) string {
	p := float64(count) / float64(total) * 100
	switch {
	case p == 0:
		return "0%"
	case p < .5:
		return fmt.Sprintf("%.1f%%", p)[1:]
	default:
		return fmt.Sprintf("%.0f%%", math.Round(p))
	}
}

func elideCenter(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 2 {
		return string(r[:max(0, n)])
	}
	left := (n - 1) / 2
	right := n - 1 - left
	return string(r[:left]) + "…" + string(r[len(r)-right:])
}
