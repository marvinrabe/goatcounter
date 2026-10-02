package goatcounter

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/parse"
)

// visitorRows selects the first matching pageview in each session. This is the
// same visit definition used by dashboard_metrics.Series; events do not create
// visits or contribute to visitor breakdowns.
const visitorRows = `
with ranked as (
	select hits.*,
		row_number() over (partition by hits.session order by datetime(hits.created_at), hits.hit_id) as visit_row
	from hits
	join paths using (path_id)
	where datetime(hits.created_at) >= datetime(:start)
		and datetime(hits.created_at) <= datetime(:end)
		and paths.event = 0 and :filter
), visits as (select * from ranked where visit_row = 1)
`

// ListVisitorBreakdown returns visits attributed to one value of a dashboard
// dimension. A session can appear in only one row of each top-level breakdown.
func (h *HitStats) ListVisitorBreakdown(ctx context.Context, kind, detail string, rng datetime.Range, pathFilter PathFilter, limit, offset int) error {
	var query string
	switch kind {
	case "browsers":
		if detail == "" {
			query = `select browsers.name as name, count(*) as count from visits
				join browsers using (browser_id) group by browsers.name
				order by count desc, name asc limit :limit offset :offset`
		} else {
			query = `select trim(browsers.name || ' ' || browsers.version) as name, count(*) as count from visits
				join browsers using (browser_id) where lower(browsers.name) = lower(:detail)
				group by browsers.name, browsers.version order by count desc, name asc limit :limit offset :offset`
		}
	case "systems":
		if detail == "" {
			query = `select systems.name as name, count(*) as count from visits
				join systems using (system_id) group by systems.name
				order by count desc, name asc limit :limit offset :offset`
		} else {
			query = `select trim(systems.name || ' ' || systems.version) as name, count(*) as count from visits
				join systems using (system_id) where lower(systems.name) = lower(:detail)
				group by systems.name, systems.version order by count desc, name asc limit :limit offset :offset`
		}
	case "locations":
		if detail == "" {
			query = `select locations.iso_3166_2 as id, locations.country_name as name, count(*) as count from visits
				join locations on locations.iso_3166_2 = substr(visits.location, 1, 2)
				group by locations.iso_3166_2, locations.country_name
				order by count desc, id asc limit :limit offset :offset`
		} else {
			query = `select coalesce(locations.region_name, '(unknown)') as name, count(*) as count from visits
				join locations on locations.iso_3166_2 = visits.location
				where locations.country = :detail group by locations.iso_3166_2, name
				order by count desc, name asc limit :limit offset :offset`
		}
	case "languages":
		query = `select languages.iso_639_3 as id, languages.name as name, count(*) as count from visits
			join languages on languages.iso_639_3 = visits.language
			group by languages.iso_639_3, languages.name
			order by count desc, id asc limit :limit offset :offset`
	case "sizes":
		query = `select cast(coalesce(visits.width, 0) as integer) as name, count(*) as count from visits
			group by name order by count desc, name asc`
		if detail != "" {
			query = `select '↔ ' || cast(coalesce(visits.width, 0) as integer) || 'px' as name, count(*) as count from visits
				where ((:detail = 'unknown' and (visits.width is null or visits.width = 0))
					or (:detail = 'phone' and visits.width > 0 and visits.width <= 600)
					or (:detail = 'tablet' and visits.width > 600 and visits.width <= 1000)
					or (:detail = 'desktop' and visits.width > 1000))
				group by cast(visits.width as integer) order by count desc, name asc limit :limit offset :offset`
		}
	case "campaigns":
		if detail == "" {
			query = `select cast(campaigns.campaign_id as text) as id, campaigns.name as name, count(*) as count from visits
				join campaigns on campaigns.campaign_id = visits.campaign
				group by campaigns.campaign_id, campaigns.name
				order by count desc, id asc limit :limit offset :offset`
		} else {
			query = `select coalesce(refs.ref, '') as name, count(*) as count from visits
				left join refs using (ref_id) where visits.campaign = :detail
				group by refs.ref order by count desc, name asc limit :limit offset :offset`
		}
	case "toprefs":
		query = `select coalesce(refs.ref, '') as name, refs.ref_scheme as ref_scheme, count(*) as count from visits
			left join refs using (ref_id)
			where (:has_domain = 0 or refs.ref not like :own_ref)
			group by visits.ref_id order by count desc, name asc limit :limit offset :offset`
	case "refpaths":
		query = `select paths.path as name, count(distinct matched.session) as count from ranked matched
			join visits on visits.session = matched.session
			join refs on refs.ref_id = visits.ref_id
			join paths on paths.path_id = matched.path_id
			where lower(refs.ref) = lower(:detail)
			group by matched.path_id, paths.path order by count desc, name asc limit :limit offset :offset`
	case "pagerefs":
		// A page can be reached more than once in a session. Attribute it to
		// the first matching page hit so the referral rows sum to its count.
		query = `select coalesce(refs.ref, '') as name, refs.ref_scheme as ref_scheme, count(*) as count from (
				select visits.ref_id, row_number() over (partition by visits.session order by datetime(visits.created_at), visits.hit_id) as path_row
				from ranked visits where visits.path_id = :detail
			) page_visits left join refs using (ref_id) where path_row = 1
			group by page_visits.ref_id order by count desc, name asc limit :limit offset :offset`
	default:
		return fmt.Errorf("unknown visitor breakdown: %s", kind)
	}
	filterSQL, filterParams := pathFilter.SQL(ctx, "hits")
	site := MustGetSite(ctx)
	err := database.Select(ctx, &h.Stats, visitorRows+query, filterParams, map[string]any{
		"start": rng.Start, "end": rng.End, "filter": filterSQL,
		"detail": detail, "limit": limit + 1, "offset": offset,
		"has_domain": site.LinkDomain != "", "own_ref": site.LinkDomainURL(false) + "%",
	})
	if err != nil {
		return fmt.Errorf("ListVisitorBreakdown(%s): %w", kind, err)
	}
	if kind == "sizes" && detail != "" {
		for i := range h.Stats {
			h.Stats[i].Name = strings.ReplaceAll(h.Stats[i].Name, "↔", "↔\ufe0e")
		}
	}
	if (kind != "sizes" || detail != "") && len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:limit]
	}
	return nil
}

// ListVisitorSizes groups visit-level widths into the four dashboard device
// categories, using the same boundaries as ListSizes.
func (h *HitStats) ListVisitorSizes(ctx context.Context, rng datetime.Range, pathFilter PathFilter, sortByCount bool) error {
	if err := h.ListVisitorBreakdown(ctx, "sizes", "", rng, pathFilter, 0, 0); err != nil {
		return err
	}
	ns := []HitStat{{ID: SizePhones}, {ID: SizeTablets}, {ID: SizeDesktop}, {ID: SizeUnknown}}
	for _, stat := range h.Stats {
		width, _ := parse.Int[int16](stat.Name, 10)
		switch {
		case width == 0:
			ns[3].Count += stat.Count
		case width <= 600:
			ns[0].Count += stat.Count
		case width <= 1000:
			ns[1].Count += stat.Count
		default:
			ns[2].Count += stat.Count
		}
	}
	if sortByCount {
		slices.SortFunc(ns, func(a, b HitStat) int { return cmp.Compare(b.Count, a.Count) })
	}
	h.Stats = ns
	h.More = false
	return nil
}

// ListVisitorPages counts a session once on each page it reached. The first
// matching hit to a page determines the chart bucket and referral drilldown.
func (h *HitLists) ListVisitorPages(ctx context.Context, rng datetime.Range, pathFilter PathFilter, exclude []PathID, limit int, group Group, withStats bool) (int, bool, error) {
	filterSQL, filterParams := pathFilter.SQL(ctx, "hits")
	query := `with ranked as (
		select hits.session, hits.path_id, hits.created_at,
			row_number() over (partition by hits.session, hits.path_id order by datetime(hits.created_at), hits.hit_id) as path_row
		from hits join paths using (path_id)
		where datetime(hits.created_at) >= datetime(:start)
			and datetime(hits.created_at) <= datetime(:end)
			and paths.event = 0 and :filter
			{{if .exclude}}and hits.path_id not in (:exclude){{end}}
	), hourly as (
		select path_id, substr(datetime(created_at, :offset2), 0, 14) as hour, count(*) as total
		from ranked where path_row = 1 group by path_id, hour
	), page_counts as (
		select path_id, sum(total) as count, json_group_object(hour, total) as stats2
		from hourly group by path_id order by count desc, path_id desc limit :limit
	)
	select page_counts.path_id, paths.path, paths.event, page_counts.count,
		coalesce(page_counts.stats2, '{}') as stats2
	from page_counts join paths using (path_id) order by count desc, path_id desc`
	err := database.Select(ctx, h, query, filterParams, map[string]any{
		"start": rng.Start, "end": rng.End, "filter": filterSQL,
		"exclude": exclude, "limit": limit + 1,
		"offset2": fmt.Sprintf("%d minutes", Config(ctx).Timezone.Offset()),
	})
	if err != nil {
		return 0, false, fmt.Errorf("ListVisitorPages: %w", err)
	}
	more := len(*h) > limit
	if more {
		*h = (*h)[:limit]
	}
	var displayed int
	for i := range *h {
		if withStats {
			(*h)[i].sum(ctx, rng, group)
		} else {
			(*h)[i].Stats2 = nil
		}
		displayed += (*h)[i].Count
	}
	return displayed, more, nil
}
