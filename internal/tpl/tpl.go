// Package tpl registers GoatCounter's HTML template functions.
package tpl

import (
	"context"
	"encoding/base32"
	"fmt"
	"html/template"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/i18n"
	"zgo.at/zhttp"
	"zgo.at/zstd/zstring"
	"zgo.at/zstd/ztime"
	"zgo.at/ztpl/tplfunc"
	"zgo.at/zvalidate"
)

func init() {
	tplfunc.Add("concat", func(sep string, strs ...string) string {
		return strings.Join(strs, sep)
	})
	tplfunc.Add("percentage", func(n, total int) float64 {
		return float64(n) / float64(total) * 100
	})
	tplfunc.Add("ago", func(t time.Time) time.Duration {
		return time.Since(t).Round(time.Second)
	})
	tplfunc.Add("repeat", strings.Repeat)
	tplfunc.Add("center", zstring.AlignCenter)
	tplfunc.Add("trim_left", strings.TrimLeft)
	tplfunc.Add("trim_right", strings.TrimRight)

	tplfunc.Add("round_duration", func(d time.Duration) time.Duration {
		if d < time.Millisecond {
			return d
		}
		if d < time.Second*10 {
			return d.Round(time.Millisecond * 100)
		}
		return d.Round(time.Second)
	})

	tplfunc.Add("distribute_durations", func(times ztime.Durations, n int) template.HTML {
		p := func(d time.Duration) string {
			return ztime.DurationAs(d.Round(time.Millisecond), time.Millisecond)
		}
		b := new(strings.Builder)

		fmt.Fprintln(b, "\nDistribution:")
		dist := times.Distrubute(n)
		var (
			widthDur, widthNum int
			widthBar           = 100.0
		)
		for _, d := range dist {
			if l := len(p(d.Min())); l > widthDur {
				widthDur = l
			}
			if l := len(strconv.Itoa(d.Len())); l > widthNum {
				widthNum = l
			}
		}

		format := fmt.Sprintf("    ≤ %%%ds ms → %%%dd  %%s %%.1f%%%%\n", widthDur, widthNum)
		l := float64(times.Len())
		for _, h := range dist {
			if h.Len() == 0 {
				continue
			}
			r := int(widthBar / (l / float64(h.Len())))
			perc := float64(h.Len()) / l * 100
			fmt.Fprintf(b, format, p(h.Max()), h.Len(), strings.Repeat("▬", r), perc)
		}

		return template.HTML(b.String())
	})

	tplfunc.Add("ord", func(n int) template.HTML {
		s := "th"
		switch n % 10 {
		case 1:
			if n%100 != 11 {
				s = "st"
			}
		case 2:
			if n%100 != 12 {
				s = "nd"
			}
		case 3:
			if n%100 != 13 {
				s = "rd"
			}
		}
		return template.HTML(strconv.Itoa(n) + "<sup>" + s + "</sup>")
	})

	tplfunc.Add("base32", base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString)
	tplfunc.Add("validate", zvalidate.TemplateError)
	tplfunc.Add("has_errors", zvalidate.TemplateHasErrors)
	tplfunc.Add("error_code", func(err error) string { return zhttp.UserErrorCode(err) })
	// Implemented as function for performance.
	tplfunc.Add("horizontal_chart", HorizontalChart)

	type x struct {
		href, label string
		items       []x
	}
	links := []x{
		{label: "Basics", items: []x{
			{href: "start", label: "Getting started"},
			{href: "visitor-counter", label: "Visitor counter"},
			{href: "events", label: "Events"},
			{href: "csp", label: "Content-Security-Policy"},
			{href: "js", label: "JavaScript API"}}},
		{label: "How can I…", items: []x{
			{href: "skip-dev", label: "Prevent tracking my own pageviews?"},
			{href: "skip-path", label: "Prevent tracking specific paths?"},
			{href: "path", label: "Control the path that's sent to GoatCounter?"},
			{href: "modify", label: "Change data before it's sent to GoatCounter?"},
			{href: "spa", label: "Add GoatCounter to a SPA?"}}},
		{label: "Other", items: []x{
			{href: "sessions", label: "Sessions and visitors"},
			{href: "faq", label: "FAQ"}}},
	}
	tplfunc.Add("help_nav", func(ctx context.Context, active string) template.HTML {
		var (
			dropdown = new(strings.Builder)
			list     = new(strings.Builder)
			w        func(context.Context, []x)
			e        = template.HTMLEscapeString
		)
		w = func(ctx context.Context, l []x) {
			for _, ll := range l {
				if len(ll.items) > 0 {
					fmt.Fprintf(list, `<li><strong>%s</strong><ul>`, e(ll.label))
					fmt.Fprintf(dropdown, `<optgroup label="%s">`, e(ll.label))
					w(ctx, ll.items)
					list.WriteString("</ul></li>")
					dropdown.WriteString("</optgroup>")
					continue
				}

				list.WriteString("<li")
				dropdown.WriteString(`<option`)
				if ll.href == active {
					list.WriteString(` class="active"`)
					dropdown.WriteString(` selected`)
				}

				fmt.Fprintf(list, `><a href="%s">%s</a></li>`, e(ll.href), e(ll.label))
				fmt.Fprintf(dropdown, ` value="%s">%s</option>`, e(ll.href), e(ll.label))
			}
		}

		dropdown.WriteString("<select>")
		list.WriteString("<ul>")
		w(ctx, links)
		dropdown.WriteString("</select>")
		list.WriteString("</ul>")
		return template.HTML(dropdown.String() + list.String())
	})
	tplfunc.Add("help_hdr", func(ctx context.Context, active string) template.HTML {
		if active == "404" {
			return "404: Not Found"
		}
		var w func(context.Context, []x) string
		w = func(ctx context.Context, l []x) string {
			for _, ll := range l {
				if ll.href == active {
					return strings.TrimRight(ll.label, "?")
				}
				if len(ll.items) > 0 {
					if r := w(ctx, ll.items); r != "" {
						return r
					}
				}
			}
			return ""
		}
		return template.HTML(w(ctx, links))
	})

	tplfunc.Add("dformat", func(ctx context.Context, t time.Time, withTime bool) string {
		f := "2006-01-02"
		if withTime {
			f += " 15:04"
		}
		return t.In(goatcounter.Config(ctx).Timezone.Loc()).Format(f)
	})

	tplfunc.Add("path_id", func(p string) string {
		p = strings.ReplaceAll(strings.TrimLeft(p, "/"), "/", "-")
		if p == "" {
			return "dashboard"
		}
		return p
	})

	tplfunc.Add("tformat", func(ctx context.Context, t time.Time, fmt string) string {
		if fmt == "" {
			fmt = "2006-01-02"
		}
		return t.In(goatcounter.Config(ctx).Timezone.Loc()).Format(fmt)
	})
	tplfunc.Add("nformat", func(n any) string {
		return tplfunc.Number(n, 0x202f)
	})

}

func HorizontalChart(ctx context.Context, stats goatcounter.HitStats, total int, link, paginate bool) template.HTML {
	if total == 0 || len(stats.Stats) == 0 {
		return template.HTML("<em>" + i18n.T(ctx, "dashboard/nothing-to-display|Nothing to display") + "</em>")
	}

	var (
		displayed int
		b         = new(strings.Builder)
	)
	b.WriteString(`<div class="rows">`)
	for _, s := range stats.Stats {
		displayed += s.Count

		var (
			p    = float64(s.Count) / float64(total) * 100
			perc string
		)
		switch {
		case p == 0:
			perc = "0%"
		case p < .5:
			perc = fmt.Sprintf("%.1f%%", p)[1:]
		default:
			perc = fmt.Sprintf("%.0f%%", math.Round(p))
		}

		name := ""
		if s.Name != "" {
			name = template.HTMLEscapeString(s.Name)
		} else {
			switch s.ID {
			case goatcounter.SizePhones:
				name = i18n.T(ctx, "label/size-phones|Phones")
			case goatcounter.SizeTablets:
				name = i18n.T(ctx, "label/size-tablets|Tablets")
			case goatcounter.SizeDesktop:
				name = i18n.T(ctx, "label/size-desktop|Desktop")
			}
		}

		unknown := false
		if name == "" {
			name = i18n.T(ctx, "unknown|(unknown)")
			unknown = true
		}
		class := ""
		if unknown || (s.RefScheme != nil && string(*s.RefScheme) == goatcounter.RefSchemeGenerated) {
			class = "generated"
		}
		visit := ""
		if !link && s.RefScheme != nil && string(*s.RefScheme) == goatcounter.RefSchemeHTTP {
			visit = fmt.Sprintf(
				`<sup class="go"><a rel="noopener" target="_blank" href="http://%s">visit</a></sup>`,
				name)
		}

		if strings.HasPrefix(name, "twitter.com/search?q=") {
			if i := strings.LastIndex(name, "t.co%2F"); i > -1 {
				name = "Twitter link: t.co/" + name[i+7:]
			}
		}

		ename := zstring.ElideCenter(name, 76)
		var ref string
		if link && !unknown {
			ref = fmt.Sprintf(`<a href="#" class="load-detail">`+
				`<span class="bar" style="width: %s"></span>`+
				`<span class="bar-c"><span class="cutoff">%s</span> %s</span></a>`, perc, ename, visit)
		} else {
			ref = fmt.Sprintf(`<span class="bar" style="width: %s"></span>`+
				`<span class="bar-c"><span class="cutoff">%s</span> %s</span>`, perc, ename, visit)
		}

		ncol := tplfunc.Number(s.Count, 0x202f)

		id := s.ID
		if id == "" {
			id = name
		}
		fmt.Fprintf(b, `
			<div class="%[1]s" data-key="%[2]s">
				<span class="col-count col-perc">%[3]s</span>
				<span class="col-name">%[4]s</span>
				<span class="col-count">%[5]s</span>
			</div>`,
			class, id, perc, ref, ncol)
	}
	b.WriteString(`</div>`)

	// Add pagination link.
	if paginate && stats.More {
		b.WriteString(`<a href="#" class="load-more">`)
		b.WriteString(i18n.T(ctx, "link/show-more|Show more"))
		b.WriteString("</a>")
		b.WriteString(`<a href="#" class="load-less">`)
		b.WriteString(i18n.T(ctx, "link/show-less|(show less)"))
		b.WriteString("</a>")
	}

	return template.HTML(b.String())
}
