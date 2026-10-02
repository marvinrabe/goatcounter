package goatcounter

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/enrich"
)

// visitsCTE selects the matching pageviews as "hits", and the first of them
// in each session as "visits". A visit takes its source, browser, location,
// etc. from that first pageview. Custom events never create visits.
//
// SQLite takes the bare columns of an aggregate query with a single min()
// from the row with the minimum, so this needs no window functions.
const visitsCTE = `
with hits as (
	select * from events
	where site = :site and aggregate = '' and ts >= :start and ts <= :end and name = 'pageview' and :filter
), visits as (
	select session, min(ts) as ts, hostname, path, source, referrer,
		utm_source, utm_medium, utm_campaign, utm_content, utm_term,
		browser, browser_version, os, os_version, width,
		country, language
	from hits group by session
)`

type breakdownQuery struct {
	live     string // Selects id, name, count from collected rows.
	migrated string // Selects id, name, count from migrated rows; may be empty.
	kind     string // Aggregate of the migrated rows.
}

// sameColumns is a breakdown that selects the id and name expressions from
// the collected visits, and from the migrated rows of kind, summing metric.
func sameColumns(id, name, where, kind, metric string) breakdownQuery {
	if where == "" {
		where = "1=1"
	}
	return breakdownQuery{
		live: `select ` + id + ` as id, ` + name + ` as name, count(*) as count
			from visits where ` + where + ` group by 1, 2`,
		migrated: `select ` + id + ` as id, ` + name + ` as name, sum(` + metric + `) as count
			from events where :migrated and ` + where + ` group by 1, 2`,
		kind: kind,
	}
}

func breakdown(kind, detail string) (breakdownQuery, error) {
	switch kind {
	case "browsers":
		if detail == "" {
			return sameColumns("''", "browser", "", "browsers", "visits"), nil
		}
		return sameColumns("''", "trim(browser || ' ' || browser_version)",
			"lower(browser) = lower(:detail)", "browsers", "visits"), nil
	case "systems":
		if detail == "" {
			return sameColumns("''", "os", "", "operating_systems", "visits"), nil
		}
		return sameColumns("''", "trim(os || ' ' || os_version)",
			"lower(os) = lower(:detail)", "operating_systems", "visits"), nil
	case "locations":
		return sameColumns("country", "country", "", "locations", "visits"), nil
	case "languages":
		// Plausible doesn't record languages.
		q := sameColumns("language", "language", "language <> ''", "", "")
		q.migrated = ""
		return q, nil
	case "toprefs":
		return sameColumns("''", "coalesce(nullif(source, ''), referrer)", "", "sources", "visits"), nil
	case "campaigns":
		if detail == "" {
			return sameColumns("utm_campaign", "utm_campaign", "utm_campaign <> ''", "sources", "visits"), nil
		}
		return sameColumns("''", "coalesce(nullif(referrer, ''), source)",
			"lower(utm_campaign) = lower(:detail)", "sources", "visits"), nil
	case "utm_mediums":
		return sameColumns("''", "utm_medium", "utm_medium <> ''", "sources", "visits"), nil
	case "utm_sources":
		return sameColumns("''", "utm_source", "utm_source <> ''", "sources", "visits"), nil
	case "entry_pages":
		return sameColumns("''", "path", "", "entry_pages", "entrances"), nil
	case "sizes":
		if detail == "" {
			return breakdownQuery{
				live:     `select ` + enrich.DeviceFromWidth + ` as id, '' as name, count(*) as count from visits group by 1`,
				migrated: `select ` + enrich.DeviceFromPlausible + ` as id, '' as name, sum(visits) as count from events where :migrated group by 1`,
				kind:     "devices",
			}, nil
		}
		return breakdownQuery{
			live: `select '' as id, '↔' || char(0xfe0e) || ' ' || width || 'px' as name, count(*) as count
				from visits where ` + enrich.DeviceFromWidth + ` = :detail and width > 0 group by width`,
			migrated: `select '' as id, device as name, sum(visits) as count
				from events where :migrated and ` + enrich.DeviceFromPlausible + ` = :detail group by 2`,
			kind: "devices",
		}, nil
	case "exit_pages":
		return breakdownQuery{
			live: `select '' as id, path as name, count(*) as count from (
					select session, max(ts), path from hits group by session
				) group by 2`,
			migrated: `select '' as id, path as name, sum(exits) as count from events
				where :migrated group by 2`,
			kind: "exit_pages",
		}, nil
	case "events":
		// Unique visitors with the event, as in Plausible.
		return breakdownQuery{
			live: `select '' as id, name, count(distinct visitor) as count from events
				where site = :site and aggregate = '' and ts >= :start and ts <= :end and name <> 'pageview' and :filter
				group by 2`,
			// Plausible's own "engagement" events measure scroll depth and
			// time on page, and aren't custom events.
			migrated: `select '' as id, name, sum(visitors) as count from events
				where :migrated and name <> 'engagement' group by 2`,
			kind: "custom_events",
		}, nil
	case "refpaths":
		// The pages reached in visits from this source.
		return breakdownQuery{
			live: `select '' as id, hits.path as name, count(distinct hits.session) as count
				from hits join visits using (session)
				where lower(coalesce(nullif(visits.source, ''), visits.referrer)) = lower(:detail)
				group by 2`,
		}, nil
	case "pagerefs":
		// The source of the first pageview of this page in each visit, so
		// the rows add up to the page's visits.
		return breakdownQuery{
			live: `select '' as id, coalesce(nullif(source, ''), referrer) as name, count(*) as count from (
					select session, min(ts), source, referrer from hits
					where path = :detail group by session
				) group by 2`,
		}, nil
	}
	return breakdownQuery{}, fmt.Errorf("unknown visitor breakdown: %s", kind)
}

// ListVisitorBreakdown returns visits attributed to one value of a dashboard
// dimension. A visit appears in only one row of each top-level breakdown.
//
// Migrated Plausible rows are only included when the filter matches every
// pageview, as Plausible exports don't break down dimensions by page.
func (h *HitStats) ListVisitorBreakdown(ctx context.Context, kind, detail string, rng datetime.Range, pathFilter PathFilter, limit, offset int) error {
	q, err := breakdown(kind, detail)
	if err != nil {
		return err
	}
	params := rangeParams(ctx, rng, pathFilter, "path", "name")
	params["detail"] = detail
	params["limit"] = limit + 1
	params["offset"] = offset
	params["kind"] = q.kind
	params["migrated"] = database.SQL(`site = :site and aggregate = :kind and ts >= :start and ts <= :end`)
	if limit <= 0 {
		params["limit"] = -1
	}

	union := q.live
	if q.migrated != "" && pathFilter.AllPageviews() {
		union += "\nunion all\n" + q.migrated
	}
	// Collected and migrated rows with the same ID (or name) are one row.
	err = database.Select(ctx, &h.Stats, visitsCTE+`
		select min(id) as id, min(name) as name, sum(count) as count from (`+union+`)
		group by case when id <> '' then lower(id) else lower(name) end
		order by count desc, name asc
		limit :limit offset :offset`, params)
	if err != nil {
		return fmt.Errorf("ListVisitorBreakdown(%s): %w", kind, err)
	}
	h.More = limit > 0 && len(h.Stats) > limit
	if h.More {
		h.Stats = h.Stats[:limit]
	}

	for i := range h.Stats {
		s := &h.Stats[i]
		switch kind {
		case "locations":
			if n := enrich.CountryName(s.Name); n != "" {
				s.Name = n
			}
		case "languages":
			if n := enrich.LanguageName(s.ID); n != "" {
				s.Name = n
			}
		case "toprefs", "pagerefs", "campaigns":
			if kind == "campaigns" && detail == "" {
				continue
			}
			scheme := RefSchemeGenerated
			if strings.Contains(s.Name, ".") && !strings.Contains(s.Name, " ") {
				scheme = RefSchemeHTTP
			}
			s.RefScheme = &scheme
		}
	}
	return nil
}

// ListVisitorSizes groups visits into the four dashboard device categories.
func (h *HitStats) ListVisitorSizes(ctx context.Context, rng datetime.Range, pathFilter PathFilter, sortByCount bool) error {
	if err := h.ListVisitorBreakdown(ctx, "sizes", "", rng, pathFilter, 0, 0); err != nil {
		return err
	}
	ns := []HitStat{{ID: SizePhones}, {ID: SizeTablets}, {ID: SizeDesktop}, {ID: SizeUnknown}}
	for _, stat := range h.Stats {
		for i := range ns {
			if ns[i].ID == stat.ID {
				ns[i].Count += stat.Count
			}
		}
	}
	if sortByCount {
		slices.SortStableFunc(ns, func(a, b HitStat) int { return cmp.Compare(b.Count, a.Count) })
	}
	h.Stats, h.More = ns, false
	return nil
}

// ListVisitorPages counts the unique visitors of each page, as Plausible
// does; visitors are unique per day.
func (h *HitLists) ListVisitorPages(ctx context.Context, rng datetime.Range, pathFilter PathFilter, limit, offset int) (int, bool, error) {
	params := rangeParams(ctx, rng, pathFilter, "path", "name")
	iparams := rangeParams(ctx, rng, pathFilter, "path", "")
	params["ifilter"] = iparams["filter"]
	params["limit"] = limit + 1
	params["offset"] = offset

	err := database.Select(ctx, h, `
		with pages as (
			select path, count(distinct visitor) as n from events
			where site = :site and aggregate = '' and ts >= :start and ts <= :end and name = 'pageview' and :filter
			group by path
			union all
			select path, sum(visitors) from events
			where site = :site and aggregate = 'pages' and ts >= :start and ts <= :end and :ifilter
			group by path
		)
		select path, sum(n) as count from pages
		group by path
		order by count desc, path asc
		limit :limit offset :offset`, params)
	if err != nil {
		return 0, false, fmt.Errorf("ListVisitorPages: %w", err)
	}
	more := len(*h) > limit
	if more {
		*h = (*h)[:limit]
	}
	var displayed int
	for _, p := range *h {
		displayed += p.Count
	}
	return displayed, more, nil
}
