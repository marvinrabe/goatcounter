package analytics

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/marvinrabe/goatcounter/internal/enrich"
)

// BreakdownRow is the number of visits for one value of a dimension.
type BreakdownRow struct {
	// ID for selecting more details; not present in the detail view.
	ID    string `db:"id"`
	Name  string `db:"name"`  // Display name.
	Count int    `db:"count"` // Number of visits.

	// What kind of referral this is; only set when retrieving referrals {enum: h g}.
	//
	//  h   A domain or URL, which can be linked.
	//  g   Generated; for example "Google" or a utm_source value.
	RefScheme *string `db:"-"`
}

// Breakdown is the visits by one dimension, such as browsers.
type Breakdown struct {
	More bool // There are more rows than the limit.
	Rows []BreakdownRow
}

const (
	SizePhones  = "phone"
	SizeTablets = "tablet"
	SizeDesktop = "desktop"
	SizeUnknown = "unknown"
)

// BreakdownRow.RefScheme values.
const (
	RefSchemeHTTP      = "h"
	RefSchemeGenerated = "g"
)

// Page is the number of visitors of a path.
type Page struct {
	Path  string `db:"path"`
	Count int    `db:"count"`
}

// visitsCTE selects the pageviews matching filter as "views", and the first of them
// in each session as "visits". A visit takes its source, browser, location,
// etc. from that first pageview. Custom events never create visits.
//
// SQLite takes the bare columns of an aggregate query with a single min()
// from the row with the minimum, so this needs no window functions.
func visitsCTE(filter string) string {
	return `
with views as (
	select * from events
	where site = :site and aggregate = '' and ts >= :start and ts <= :end and name = 'pageview' and ` + filter + `
), visits as (
	select session, min(ts) as ts, hostname, path, source, referrer,
		utm_source, utm_medium, utm_campaign, utm_content, utm_term,
		browser, browser_version, os, os_version, device,
		country, language
	from views group by session
)`
}

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
			from events where ` + migratedRows + ` and ` + where + ` group by 1, 2`,
		kind: kind,
	}
}

// migratedRows selects the migrated rows of a breakdown's kind.
const migratedRows = `site = :site and aggregate = :kind and ts >= :start and ts <= :end`

// breakdown gets the queries for a kind of breakdown; filter is the condition
// for the pageviews.
func breakdown(kind, detail, filter string) (breakdownQuery, error) {
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
		return sameColumns("device", "''", "", "devices", "visits"), nil
	case "exit_pages":
		return breakdownQuery{
			live: `select '' as id, path as name, count(*) as count from (
					select session, max(ts), path from views group by session
				) group by 2`,
			migrated: `select '' as id, path as name, sum(exits) as count from events
				where ` + migratedRows + ` group by 2`,
			kind: "exit_pages",
		}, nil
	case "events":
		// Unique visitors with the event, as in Plausible.
		return breakdownQuery{
			live: `select '' as id, name, count(distinct visitor) as count from events
				where site = :site and aggregate = '' and ts >= :start and ts <= :end and name <> 'pageview' and ` + filter + `
				group by 2`,
			// Plausible's own "engagement" events measure scroll depth and
			// time on page, and aren't custom events.
			migrated: `select '' as id, name, sum(visitors) as count from events
				where ` + migratedRows + ` and name <> 'engagement' group by 2`,
			kind: "custom_events",
		}, nil
	case "refpaths":
		// The pages reached in visits from this source.
		return breakdownQuery{
			live: `select '' as id, views.path as name, count(distinct views.session) as count
				from views join visits using (session)
				where lower(coalesce(nullif(visits.source, ''), visits.referrer)) = lower(:detail)
				group by 2`,
		}, nil
	case "pagerefs":
		// The source of the first pageview of this page in each visit, so
		// the rows add up to the page's visits.
		return breakdownQuery{
			live: `select '' as id, coalesce(nullif(source, ''), referrer) as name, count(*) as count from (
					select session, min(ts), source, referrer from views
					where path = :detail group by session
				) group by 2`,
		}, nil
	}
	return breakdownQuery{}, fmt.Errorf("unknown visitor breakdown: %s", kind)
}

// Breakdown returns visits attributed to one value of a dashboard dimension;
// detail selects the rows of one browser, system, etc. A visit appears in only one row of each top-level breakdown.
//
// Migrated Plausible rows are only included when the filter matches every
// pageview, as Plausible exports don't break down dimensions by page.
func (s *Store) Breakdown(ctx context.Context, query Query, kind, detail string, limit, offset int) (Breakdown, error) {
	var h Breakdown
	filter, params := query.params("path", "name")
	q, err := breakdown(kind, detail, filter)
	if err != nil {
		return h, err
	}
	params["detail"] = detail
	params["limit"] = limit + 1
	params["offset"] = offset
	params["kind"] = q.kind
	if limit <= 0 {
		params["limit"] = -1
	}

	union := q.live
	if q.migrated != "" && query.Filter.AllPageviews() {
		union += "\nunion all\n" + q.migrated
	}
	// Collected and migrated rows with the same ID (or name) are one row.
	err = s.DB.Select(ctx, &h.Rows, visitsCTE(filter)+`
		select min(id) as id, min(name) as name, sum(count) as count from (`+union+`)
		group by case when id <> '' then lower(id) else lower(name) end
		order by count desc, name asc
		limit :limit offset :offset`, named(params)...)
	if err != nil {
		return h, fmt.Errorf("Breakdown(%s): %w", kind, err)
	}
	h.More = limit > 0 && len(h.Rows) > limit
	if h.More {
		h.Rows = h.Rows[:limit]
	}

	for i := range h.Rows {
		st := &h.Rows[i]
		switch kind {
		case "locations":
			if n := enrich.CountryName(st.Name); n != "" {
				st.Name = n
			}
		case "languages":
			if n := enrich.LanguageName(st.ID); n != "" {
				st.Name = n
			}
		case "toprefs", "pagerefs", "campaigns":
			if kind == "campaigns" && detail == "" {
				continue
			}
			scheme := RefSchemeGenerated
			if strings.Contains(st.Name, ".") && !strings.Contains(st.Name, " ") {
				scheme = RefSchemeHTTP
			}
			st.RefScheme = &scheme
		}
	}
	return h, nil
}

// sizes is the dashboard device category of each device name; others are
// unknown.
var sizes = map[string]string{
	enrich.DeviceMobile:  SizePhones,
	enrich.DeviceTablet:  SizeTablets,
	enrich.DeviceLaptop:  SizeDesktop,
	enrich.DeviceDesktop: SizeDesktop,
}

// Sizes groups visits into the four dashboard device categories.
func (s *Store) Sizes(ctx context.Context, q Query, sortByCount bool) (Breakdown, error) {
	h, err := s.Breakdown(ctx, q, "sizes", "", 0, 0)
	if err != nil {
		return h, err
	}
	ns := []BreakdownRow{{ID: SizePhones}, {ID: SizeTablets}, {ID: SizeDesktop}, {ID: SizeUnknown}}
	for _, stat := range h.Rows {
		size, ok := sizes[stat.ID]
		if !ok {
			size = SizeUnknown
		}
		for i := range ns {
			if ns[i].ID == size {
				ns[i].Count += stat.Count
			}
		}
	}
	if sortByCount {
		slices.SortStableFunc(ns, func(a, b BreakdownRow) int { return cmp.Compare(b.Count, a.Count) })
	}
	h.Rows, h.More = ns, false
	return h, nil
}

// Pages counts the unique visitors of each page, as Plausible does; visitors
// are unique per day. It also reports if there are more pages.
func (s *Store) Pages(ctx context.Context, q Query, limit, offset int) ([]Page, bool, error) {
	var h []Page
	filter, params := q.params("path", "name")
	pageFilter, _ := q.params("path", "")
	params["limit"] = limit + 1
	params["offset"] = offset

	err := s.DB.Select(ctx, &h, `
		with pages as (
			select path, count(distinct visitor) as n from events
			where site = :site and aggregate = '' and ts >= :start and ts <= :end and name = 'pageview' and `+filter+`
			group by path
			union all
			select path, sum(visitors) from events
			where site = :site and aggregate = 'pages' and ts >= :start and ts <= :end and `+pageFilter+`
			group by path
		)
		select path, sum(n) as count from pages
		group by path
		order by count desc, path asc
		limit :limit offset :offset`, named(params)...)
	if err != nil {
		return nil, false, fmt.Errorf("Pages: %w", err)
	}
	more := len(h) > limit
	if more {
		h = h[:limit]
	}
	return h, more, nil
}
