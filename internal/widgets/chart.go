package widgets

import (
	"fmt"
	"math"
	"strings"

	"github.com/marvinrabe/goatcounter"
)

// Chart is a horizontal bar chart; html/template owns all markup and escaping.
type Chart struct {
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
	Detail     *Chart // Referrers below a path.
}

// Names for rows without a name.
const (
	nameUnknown = "(unknown)"
	nameDirect  = "Direct / none"
)

// newChart creates a chart for stats; link makes the rows load their detail
// when clicked, and unnamed rows are shown as unnamed.
func newChart(stats goatcounter.HitStats, total int, link bool, unnamed string, rowsOnly bool) Chart {
	data := Chart{More: stats.More, RowsOnly: rowsOnly}
	if total == 0 {
		return data
	}
	for _, s := range stats.Stats {
		name := s.Name
		if name == "" {
			switch s.ID {
			case goatcounter.SizePhones:
				name = "Phones"
			case goatcounter.SizeTablets:
				name = "Tablets"
			case goatcounter.SizeDesktop:
				name = "Desktop"
			}
		}
		unknown := name == ""
		if unknown {
			name = unnamed
		}
		row := ChartRow{Key: s.ID, Count: s.Count, Percentage: percentage(s.Count, total), Link: link && !unknown}
		if unknown || (s.RefScheme != nil && *s.RefScheme == goatcounter.RefSchemeGenerated) {
			row.Class = "generated"
		}
		if !link && s.RefScheme != nil && *s.RefScheme == goatcounter.RefSchemeHTTP {
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
		data.Rows = append(data.Rows, row)
	}
	return data
}

// pagesChart creates the chart for the pages list, with the referrers below
// the showRefs path.
func pagesChart(pages goatcounter.HitLists, total int, showRefs string, refs goatcounter.HitStats, rowsOnly bool) Chart {
	data := Chart{Pages: true, RowsOnly: rowsOnly}
	if total == 0 {
		return data
	}
	for _, page := range pages {
		row := ChartRow{Page: true, Link: true, Key: page.Path, Name: elideCenter(page.Path, 76),
			Count: page.Count, Percentage: percentage(page.Count, total)}
		if page.Path == showRefs {
			row.Class = "target"
			detail := newChart(refs, page.Count, false, nameDirect, false)
			row.Detail = &detail
		}
		data.Rows = append(data.Rows, row)
	}
	return data
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
