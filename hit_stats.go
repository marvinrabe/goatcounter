package goatcounter

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"zgo.at/errors"
	"zgo.at/zdb"
	"zgo.at/zstd/zstrconv"
	"zgo.at/zstd/ztime"
)

type tbl struct {
	Table      string
	Columns    []string
	Constraint string
	Update     string

	onConflict string
}

func (t tbl) OnConflict(ctx context.Context) string {
	if t.onConflict == "" {
		t.onConflict = fmt.Sprintf("on conflict(%s) do update set\n\t%s",
			strings.ReplaceAll(t.Constraint, "#", ","), t.Update)
	}
	return t.onConflict
}

func (t tbl) Bulk(ctx context.Context) (zdb.BulkInsert, error) {
	ins, err := zdb.NewBulkInsert(ctx, t.Table, t.Columns)
	if err != nil {
		return zdb.BulkInsert{}, fmt.Errorf("%q: %w", t.Table, err)
	}
	ins.OnConflict(t.OnConflict(ctx))
	return ins, nil
}

var Tables = struct {
	HitCounts, RefCounts                 tbl
	BrowserStats, SystemStats, SizeStats tbl
	LocationStats, LanguageStats         tbl
	CampaignStats                        tbl
}{
	HitCounts: tbl{
		Table:      "hit_counts",
		Columns:    []string{"path_id", "hour", "total"},
		Constraint: "path_id#hour",
		Update:     `total = hit_counts.total + excluded.total`,
	},
	RefCounts: tbl{
		Table:      "ref_counts",
		Columns:    []string{"path_id", "hour", "ref_id", "total"},
		Constraint: "path_id#ref_id#hour",
		Update:     `total = ref_counts.total + excluded.total`,
	},
	BrowserStats: tbl{
		Table:      "browser_stats",
		Columns:    []string{"path_id", "day", "browser_id", "count"},
		Constraint: "path_id#day#browser_id",
		Update:     `count = browser_stats.count + excluded.count`,
	},
	SystemStats: tbl{
		Table:      "system_stats",
		Columns:    []string{"path_id", "day", "system_id", "count"},
		Constraint: "path_id#day#system_id",
		Update:     `count = system_stats.count + excluded.count`,
	},
	LocationStats: tbl{
		Table:      "location_stats",
		Columns:    []string{"path_id", "day", "location", "count"},
		Constraint: "path_id#day#location",
		Update:     `count = location_stats.count + excluded.count`,
	},
	LanguageStats: tbl{
		Table:      "language_stats",
		Columns:    []string{"path_id", "day", "language", "count"},
		Constraint: "path_id#day#language",
		Update:     `count = language_stats.count + excluded.count`,
	},
	SizeStats: tbl{
		Table:      "size_stats",
		Columns:    []string{"path_id", "day", "width", "count"},
		Constraint: "path_id#day#width",
		Update:     `count = size_stats.count + excluded.count`,
	},
	CampaignStats: tbl{
		Table:      "campaign_stats",
		Columns:    []string{"path_id", "day", "campaign_id", "ref", "count"},
		Constraint: "path_id#campaign_id#ref#day",
		Update:     `count = campaign_stats.count + excluded.count`,
	},
}

type HitStat struct {
	// ID for selecting more details; not present in the detail view.
	ID    string `db:"id" json:"id,omitempty"`
	Name  string `db:"name" json:"name"`   // Display name.
	Count int    `db:"count" json:"count"` // Number of visitors.

	// What kind of referral this is; only set when retrieving referrals {enum: h g c o}.
	//
	//  h   HTTP Referal header.
	//  g   Generated; for example are Google domains (google.com, google.nl,
	//      google.co.nz, etc.) are grouped as the generated referral "Google".
	//  c   Campaign (via query parameter)
	//  o   Other
	RefScheme *string `db:"ref_scheme" json:"ref_scheme,omitempty"`
}

type HitStats struct {
	More  bool      `json:"more"`
	Stats []HitStat `json:"stats"`
}

func asUTCDate(ctx context.Context, t time.Time) string {
	return t.In(Config(ctx).Timezone.Loc()).Format("2006-01-02")
}

// ListTopRefs lists all ref statistics for the given time period, excluding
// referrals from the configured LinkDomain.
//
// The returned count is the count without LinkDomain, and is different from the
// total number of hits.
func (h *HitStats) ListTopRefs(ctx context.Context, rng ztime.Range, pathFilter PathFilter, limit, offset int) error {
	var (
		site                    = MustGetSite(ctx)
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:ref.ListTopRefs.sql", filterParams, map[string]any{
		"start":      rng.Start,
		"end":        rng.End,
		"filter":     filterSQL,
		"ref":        site.LinkDomainURL(false) + "%",
		"limit":      limit + 1,
		"limit2":     limit + (limit * 3),
		"offset":     offset,
		"has_domain": site.LinkDomain != "",
	})
	if err != nil {
		return errors.Wrap(err, "HitStats.ListAllRefs")
	}

	if len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:len(h.Stats)-1]
	}
	return nil
}

// ListTopRef lists all paths by referrer.
func (h *HitStats) ListTopRef(ctx context.Context, ref string, rng ztime.Range, pathFilter PathFilter, limit, offset int) error {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:hit_stats.ByRef", filterParams, map[string]any{
		"start":  rng.Start,
		"end":    rng.End,
		"filter": filterSQL,
		"ref":    ref,
		"limit":  limit + 1,
		"offset": offset,
	})
	if len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:len(h.Stats)-1]
	}
	return errors.Wrap(err, "HitStats.ByRef")
}

// ListBrowsers lists all browser statistics for the given time period.
func (h *HitStats) ListBrowsers(ctx context.Context, rng ztime.Range, pathFilter PathFilter, limit, offset int) error {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:hit_stats.ListBrowsers", filterParams, map[string]any{
		"start":  asUTCDate(ctx, rng.Start),
		"end":    asUTCDate(ctx, rng.End),
		"filter": filterSQL,
		"limit":  limit + 1,
		"offset": offset,
	})
	if len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:len(h.Stats)-1]
	}
	return errors.Wrap(err, "HitStats.ListBrowsers")
}

// ListBrowser lists all the versions for one browser.
func (h *HitStats) ListBrowser(ctx context.Context, browser string, rng ztime.Range, pathFilter PathFilter, limit, offset int) error {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:hit_stats.ListBrowser", filterParams, map[string]any{
		"start":   asUTCDate(ctx, rng.Start),
		"end":     asUTCDate(ctx, rng.End),
		"filter":  filterSQL,
		"browser": browser,
		"limit":   limit + 1,
		"offset":  offset,
	})
	if len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:len(h.Stats)-1]
	}
	return errors.Wrap(err, "HitStats.ListBrowser")
}

// ListSystems lists OS statistics for the given time period.
func (h *HitStats) ListSystems(ctx context.Context, rng ztime.Range, pathFilter PathFilter, limit, offset int) error {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:hit_stats.ListSystems", filterParams, map[string]any{
		"start":  asUTCDate(ctx, rng.Start),
		"end":    asUTCDate(ctx, rng.End),
		"filter": filterSQL,
		"limit":  limit + 1,
		"offset": offset,
	})
	if len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:len(h.Stats)-1]
	}
	return errors.Wrap(err, "HitStats.ListSystems")
}

// ListSystem lists all the versions for one system.
func (h *HitStats) ListSystem(ctx context.Context, system string, rng ztime.Range, pathFilter PathFilter, limit, offset int) error {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:hit_stats.ListSystem", filterParams, map[string]any{
		"start":  asUTCDate(ctx, rng.Start),
		"end":    asUTCDate(ctx, rng.End),
		"filter": filterSQL,
		"system": system,
		"limit":  limit + 1,
		"offset": offset,
	})
	if len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:len(h.Stats)-1]
	}
	return errors.Wrap(err, "HitStats.ListSystem")
}

// ListLanguages lists all languages.
func (h *HitStats) ListLanguages(ctx context.Context, rng ztime.Range, pathFilter PathFilter, limit, offset int) error {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:hit_stats.ListLanguages", filterParams, map[string]any{
		"start":  asUTCDate(ctx, rng.Start),
		"end":    asUTCDate(ctx, rng.End),
		"filter": filterSQL,
		"limit":  limit + 1,
		"offset": offset,
	})
	if len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:len(h.Stats)-1]
	}
	return errors.Wrap(err, "HitStats.ListLanguages")
}

const (
	SizePhones  = "phone"
	SizeTablets = "tablet"
	SizeDesktop = "desktop"
	SizeUnknown = "unknown"
)

// ListSizes lists all device sizes.
func (h *HitStats) ListSizes(ctx context.Context, rng ztime.Range, pathFilter PathFilter, sortByCount bool) error {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:hit_stats.ListSizes", filterParams, map[string]any{
		"start":  asUTCDate(ctx, rng.Start),
		"end":    asUTCDate(ctx, rng.End),
		"filter": filterSQL,
	})
	if err != nil {
		return errors.Wrap(err, "HitStats.ListSize")
	}

	// Group a bit more user-friendly.
	ns := []HitStat{
		{ID: SizePhones, Count: 0},
		{ID: SizeTablets, Count: 0},
		{ID: SizeDesktop, Count: 0},
		{ID: SizeUnknown, Count: 0},
	}
	for i := range h.Stats {
		x, _ := zstrconv.ParseInt[int16](h.Stats[i].Name, 10)
		switch {
		case x == 0:
			ns[3].Count += h.Stats[i].Count
		case x <= 600:
			ns[0].Count += h.Stats[i].Count
		case x <= 1000:
			ns[1].Count += h.Stats[i].Count
		default:
			ns[2].Count += h.Stats[i].Count
		}
	}
	if sortByCount {
		slices.SortFunc(ns, func(a, b HitStat) int { return cmp.Compare(b.Count, a.Count) })
	}
	h.Stats = ns

	return nil
}

// ListSize lists all sizes for one grouping.
func (h *HitStats) ListSize(ctx context.Context, id string, rng ztime.Range, pathFilter PathFilter, limit, offset int) error {
	var (
		minSize, maxSize int
		empty            bool
	)
	switch id {
	case SizePhones:
		maxSize = 600
	case SizeTablets:
		minSize, maxSize = 600, 1000
	case SizeDesktop:
		minSize, maxSize = 1000, 99999
	case SizeUnknown:
		empty = true
	default:
		return errors.Errorf("HitStats.ListSizes: invalid value for name: %#v", id)
	}

	var (
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:hit_stats.ListSize", filterParams, map[string]any{
		"start":    asUTCDate(ctx, rng.Start),
		"end":      asUTCDate(ctx, rng.End),
		"filter":   filterSQL,
		"min_size": minSize,
		"max_size": maxSize,
		"empty":    empty,
		"limit":    limit + 1,
		"offset":   offset,
	})
	if err != nil {
		return errors.Wrap(err, "HitStats.ListSize")
	}
	if len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:len(h.Stats)-1]
	}
	for i := range h.Stats {
		h.Stats[i].Name = strings.ReplaceAll(h.Stats[i].Name, "↔", "↔\ufe0e")
	}
	return nil
}

// ListLocations lists all location statistics for the given time period.
func (h *HitStats) ListLocations(ctx context.Context, rng ztime.Range, pathFilter PathFilter, limit, offset int) error {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:hit_stats.ListLocations", filterParams, map[string]any{
		"start":  asUTCDate(ctx, rng.Start),
		"end":    asUTCDate(ctx, rng.End),
		"filter": filterSQL,
		"limit":  limit + 1,
		"offset": offset,
	})
	if len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:len(h.Stats)-1]
	}
	return errors.Wrap(err, "HitStats.ListLocations")
}

// ListLocation lists all divisions for a location
func (h *HitStats) ListLocation(ctx context.Context, country string, rng ztime.Range, pathFilter PathFilter, limit, offset int) error {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:hit_stats.ListLocation", filterParams, map[string]any{
		"start":   asUTCDate(ctx, rng.Start),
		"end":     asUTCDate(ctx, rng.End),
		"filter":  filterSQL,
		"country": country,
		"limit":   limit + 1,
		"offset":  offset,
	})
	if len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:len(h.Stats)-1]
	}
	return errors.Wrap(err, "HitStats.ListLocation")
}

// ListCampaigns lists all campaigns statistics for the given time period.
func (h *HitStats) ListCampaigns(ctx context.Context, rng ztime.Range, pathFilter PathFilter, limit, offset int) error {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:hit_stats.ListCampaigns", filterParams, map[string]any{
		"start":  asUTCDate(ctx, rng.Start),
		"end":    asUTCDate(ctx, rng.End),
		"filter": filterSQL,
		"limit":  limit + 1,
		"offset": offset,
	})
	if len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:len(h.Stats)-1]
	}
	return errors.Wrap(err, "HitStats.ListCampaigns")
}

// ListCampaign lists all statistics for a campaign.
func (h *HitStats) ListCampaign(ctx context.Context, campaign CampaignID, rng ztime.Range, pathFilter PathFilter, limit, offset int) error {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx)
	)
	err := zdb.Select(ctx, &h.Stats, "load:hit_stats.ListCampaign", filterParams, map[string]any{
		"start":    asUTCDate(ctx, rng.Start),
		"end":      asUTCDate(ctx, rng.End),
		"filter":   filterSQL,
		"campaign": campaign,
		"limit":    limit + 1,
		"offset":   offset,
	})
	if len(h.Stats) > limit {
		h.More = true
		h.Stats = h.Stats[:len(h.Stats)-1]
	}
	return errors.Wrap(err, "HitStats.ListCampaign")
}
