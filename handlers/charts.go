package handlers

import (
	"fmt"
	"math"
	"strings"

	"github.com/marvinrabe/goatcounter"
)

type chartData struct {
	Rows                  []chartRow
	Pages, RowsOnly, More bool
}

type chartRow struct {
	Key, Name, Title, Class, Percentage, VisitURL string
	Count                                         int
	PageID                                        goatcounter.PathID
	Page, Event, Link                             bool
	Detail                                        *chartData
	WidgetID                                      int
}

// Prepare chart values; html/template owns all markup and escaping.
func horizontalChart(stats goatcounter.HitStats, total int, link, paginate bool) chartData {
	data := chartData{More: paginate && stats.More}
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
			name = "(unknown)"
		}
		row := chartRow{Class: "hchart-row", Count: s.Count,
			Percentage: horizontalChartPercentage(s.Count, total), Link: link && !unknown}
		if unknown || (s.RefScheme != nil && string(*s.RefScheme) == goatcounter.RefSchemeGenerated) {
			row.Class += " generated"
		}
		if !link && s.RefScheme != nil && string(*s.RefScheme) == goatcounter.RefSchemeHTTP {
			row.VisitURL = "http://" + name
		}
		if strings.HasPrefix(name, "twitter.com/search?q=") {
			if i := strings.LastIndex(name, "t.co%2F"); i > -1 {
				name = "Twitter link: t.co/" + name[i+7:]
			}
		}
		row.Name = elideCenter(name, 76)
		row.Key = s.ID
		if row.Key == "" {
			row.Key = name
		}
		data.Rows = append(data.Rows, row)
	}
	return data
}

func horizontalChartPages(
	pages goatcounter.HitLists, total int, showRefs goatcounter.PathID,
	refs goatcounter.HitStats, widgetID int, rowsOnly bool,
) chartData {
	data := chartData{Pages: true, RowsOnly: rowsOnly}
	if total == 0 {
		return data
	}
	for _, page := range pages {
		row := chartRow{Page: true, Link: true, PageID: page.PathID,
			Name: elideCenter(page.Path, 76), Title: page.Path, Count: page.Count,
			Class: "hchart-row", Percentage: horizontalChartPercentage(page.Count, total),
			Event: bool(page.Event), WidgetID: widgetID}
		if page.Event {
			row.Class += " event"
		}
		if page.PathID == showRefs {
			row.Class += " target"
			detail := horizontalChart(refs, page.Count, false, true)
			row.Detail = &detail
		}
		data.Rows = append(data.Rows, row)
	}
	return data
}

func horizontalChartPercentage(count, total int) string {
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
