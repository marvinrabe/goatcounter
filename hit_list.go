package goatcounter

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
)

type Group uint8

func (g Group) Hourly() bool  { return g == GroupHourly }
func (g Group) Daily() bool   { return g == GroupDaily }
func (g Group) Weekly() bool  { return g == GroupWeekly }
func (g Group) Monthly() bool { return g == GroupMonthly }
func (g Group) Yearly() bool  { return g == GroupYearly }
func (g Group) String() string {
	switch g {
	case GroupDaily:
		return "day"
	case GroupWeekly:
		return "week"
	case GroupMonthly:
		return "month"
	case GroupYearly:
		return "year"
	}
	return "hour"
}

type Groups []Group

func (g Groups) Hourly() bool  { return slices.Contains(g, GroupHourly) }
func (g Groups) Daily() bool   { return slices.Contains(g, GroupDaily) }
func (g Groups) Weekly() bool  { return slices.Contains(g, GroupWeekly) }
func (g Groups) Monthly() bool { return slices.Contains(g, GroupMonthly) }
func (g Groups) Yearly() bool  { return slices.Contains(g, GroupYearly) }

const (
	GroupHourly = Group(iota)
	GroupDaily
	GroupWeekly
	GroupMonthly
	GroupYearly
)

type HitList struct {
	// Number of visitors for the selected date range.
	Count int `db:"count" json:"count"`

	// Path ID
	PathID PathID `db:"path_id" json:"path_id"`

	// Path name (e.g. /hello.html).
	Path string `db:"path" json:"path"`

	// Is this an event?
	Event bool `db:"event" json:"event"`

	// Highest visitors per hour or day (depending on daily being set).
	Max int `json:"max"`

	// Statistics by day and hour.
	Stats []HitListStat `json:"stats"`

	// What kind of referral this is; only set when retrieving referrals {enum: h g c o}.
	//
	//  h   HTTP Referal header.
	//  g   Generated; for example are Google domains (google.com, google.nl,
	//      google.co.nz, etc.) are grouped as the generated referral "Google".
	//  c   Campaign (via query parameter)
	//  o   Other
	RefScheme *string `db:"ref_scheme" json:"ref_scheme,omitempty"`

	// {nodoc}
	Stats2 Stats2 `db:"stats2" json:"-"`
}

type Stats2 map[string]int

func (s *Stats2) Scan(src any) error {
	var data []byte
	switch v := src.(type) {
	default:
		return fmt.Errorf("unknown type: %T", src)
	case []byte:
		data = v
	case string:
		data = []byte(v)
	}
	return json.Unmarshal(data, s)
}

type HitListStat struct {
	Day    string `json:"day"`    // Day these statistics are for {date}.
	Hourly []int  `json:"hourly"` // Visitors per hour.
	Daily  int    `json:"daily"`  // Total visitors for this day.

	// Visitors for the week; set once every 7 days. This value will not be set
	// if it's 0.
	Weekly int `json:"weekly,omitempty"`

	// Visitors for the month; set on first day of the month. This value will
	// not be set if it's 0.
	Monthly int `json:"monthly,omitempty"`
}

// PathCount gets the visit count for one path.
func (h *HitList) PathCount(ctx context.Context, path string, rng datetime.Range) error {
	err := database.Get(ctx, h, "load:hit_list.PathCount", map[string]any{
		"site":  MustGetSite(ctx).Key,
		"path":  path,
		"start": rng.Start,
		"end":   rng.End,
	})
	if err != nil {
		err = fmt.Errorf("HitList.PathCount: %w", err)
	}
	return err
}

// SiteTotal gets the total counts for all paths. This always uses UTC.
func (h *HitList) SiteTotalUTC(ctx context.Context, rng datetime.Range) error {
	err := database.Get(ctx, h, `/* HitList.SiteTotalUTC */
		select
			coalesce(sum(total), 0) as count
		from hit_counts
		where site = :site
		{{if not .start.IsZero}}and datetime(hour) >= datetime(:start){{end}}
		{{if not .end.IsZero}}and datetime(hour) <= datetime(:end){{end}}
	`, map[string]any{
		"site":  MustGetSite(ctx).Key,
		"start": rng.Start,
		"end":   rng.End,
	})
	if err != nil {
		err = fmt.Errorf("HitList.SiteTotalUTC: %w", err)
	}
	return err
}

func (h *HitList) sum(ctx context.Context, rng datetime.Range, group Group) int {
	var (
		total     int
		daynum    int
		week      int
		month     int
		lastmonth int
	)
	h.Stats = make([]HitListStat, 0, 7)
	for d := range rng.Add(Config(ctx).Timezone.OffsetDuration()).Iter(datetime.Day) {
		day := d.Format("2006-01-02")
		st := HitListStat{Day: day, Hourly: make([]int, 24)}
		for hour := range 24 {
			k := fmt.Sprintf("%s %02d", day, hour)
			n, ok := h.Stats2[k]
			if ok {
				st.Hourly[hour] = n
				st.Daily += n
				if group.Hourly() && st.Hourly[hour] > h.Max {
					h.Max = st.Hourly[hour]
				}
			}
		}
		if group.Daily() && st.Daily > h.Max {
			h.Max = st.Daily
		}

		if len(h.Stats) > 0 && daynum%7 == 0 { // Start of week.
			h.Stats[daynum-7].Weekly = week
			if group.Weekly() {
				h.Max = max(h.Max, week)
			}
			week = 0
		}
		if len(h.Stats) > 0 && d.Day() == 1 { // Start of month.
			h.Stats[lastmonth].Monthly = month
			if group.Monthly() {
				h.Max = max(h.Max, month)
			}
			month, lastmonth = 0, daynum
		}

		total += st.Daily
		week += st.Daily
		month += st.Daily
		h.Stats = append(h.Stats, st)
		daynum++
	}
	daynum--
	if len(h.Stats) > 0 {
		h.Stats[daynum-(daynum%7)].Weekly = week
		h.Stats[lastmonth].Monthly = month
	}
	if group.Weekly() {
		h.Max = max(h.Max, week)
	} else if group.Monthly() {
		h.Max = max(h.Max, month)
	}

	h.Stats2 = nil
	return total
}

type HitLists []HitList

// ListPathsLike lists all paths matching the like pattern.
func (h *HitLists) ListPathsLike(ctx context.Context, search string, matchCase bool) error {
	err := database.Select(ctx, h, "load:hit_list.ListPathsLike", map[string]any{
		"site":       MustGetSite(ctx).Key,
		"search":     search,
		"match_case": matchCase,
	})
	if err != nil {
		err = fmt.Errorf("Hits.ListPathsLike: %w", err)
	}
	return err
}

// List the top paths for this site in the given time period.
func (h *HitLists) List(
	ctx context.Context, rng datetime.Range, pathFilter PathFilter, exclude []PathID, limit int, group Group,
) (int, bool, error) {
	return h.list(ctx, rng, pathFilter, exclude, limit, group, true)
}

// ListCounts loads the page ranking without building unused hourly histories.
func (h *HitLists) ListCounts(
	ctx context.Context, rng datetime.Range, pathFilter PathFilter, exclude []PathID, limit int,
) (int, bool, error) {
	return h.list(ctx, rng, pathFilter, exclude, limit, GroupHourly, false)
}

func (h *HitLists) list(
	ctx context.Context, rng datetime.Range, pathFilter PathFilter, exclude []PathID, limit int, group Group, withStats bool,
) (int, bool, error) {

	var (
		filterSQL, filterParams = pathFilter.SQL(ctx, "hit_counts")
		more                    bool
	)
	{
		query := "load:hit_list.ListCounts"
		if withStats {
			query = "load:hit_list.List"
		}
		err := database.Select(ctx, h, query, filterParams, map[string]any{
			"start":   rng.Start,
			"end":     rng.End,
			"filter":  filterSQL,
			"exclude": exclude,
			"limit":   limit + 1,
			"offset":  Config(ctx).Timezone.Offset(),
			"offset2": fmt.Sprintf("%d minutes", Config(ctx).Timezone.Offset()),
		})
		if err != nil {
			return 0, false, fmt.Errorf("HitLists.List: %w", err)
		}

		// Check if there are more entries.
		if len(*h) > limit {
			hh := *h
			hh = hh[:len(hh)-1]
			*h = hh
			more = true
		}
	}

	if len(*h) == 0 { // No data (yet).
		return 0, false, nil
	}
	hh := *h

	var total int
	for i := range hh {
		if withStats {
			total += hh[i].sum(ctx, rng, group)
		} else {
			total += hh[i].Count
		}
	}
	return total, more, nil
}

// PathTotals is a special path to indicate this is the "total" overview.
//
// Trailing whitespace is trimmed on paths, so this should never conflict.
const PathTotals = "TOTAL "

// Totals gets the data for the "Totals" chart/widget.
func (h *HitList) Totals(ctx context.Context, rng datetime.Range, pathFilter PathFilter, group Group, noEvents bool) error {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx, "hit_counts")
	)
	err := database.Get(ctx, &h.Stats2, "load:hit_list.Totals", filterParams, map[string]any{
		"start":     rng.Start,
		"end":       rng.End,
		"filter":    filterSQL,
		"no_events": noEvents,
		"offset":    Config(ctx).Timezone.Offset(),
		"offset2":   fmt.Sprintf("%d minutes", Config(ctx).Timezone.Offset()),
	})
	if err != nil {
		return fmt.Errorf("HitList.Totals: %w", err)
	}

	h.Count, h.Path = h.sum(ctx, rng, group), PathTotals
	h.Max = max(h.Max, 10)
	return nil
}

type TotalCount struct {
	// Total number of visitors (including events).
	Total int `db:"total" json:"total"`
	// Total number of visitors for events.
	TotalEvents int `db:"total_events" json:"total_events"`
	// Total number of visitors in UTC. The browser, system, etc, stats are
	// always in UTC.
	TotalUTC int `db:"total_utc" json:"total_utc"`
}

// GetTotalCount gets the total number of pageviews for the selected timeview in
// the timezone the user configured.
//
// This also gets the total number of pageviews for the selected time period in
// UTC. This is needed since the _stats tables are per day, rather than
// per-hour, so we need to use the correct totals to make sure the percentage
// calculations are accurate.
//
// TotalCounter.TotalEvents is only populated if "Exclude events" is set on
// totals chart, because it's not used for anything else.
func GetTotalCount(ctx context.Context, rng datetime.Range, pathFilter PathFilter, totalEvents bool) (TotalCount, error) {
	var (
		filterSQL, filterParams = pathFilter.SQL(ctx, "hit_counts")
		t                       TotalCount
	)
	err := database.Get(ctx, &t, "load:hit_list.GetTotalCount", filterParams, map[string]any{
		"start":        rng.Start,
		"end":          rng.End,
		"start_utc":    rng.Start.In(Config(ctx).Timezone.Loc()),
		"end_utc":      rng.End.In(Config(ctx).Timezone.Loc()),
		"filter":       filterSQL,
		"tz":           Config(ctx).Timezone.Offset(),
		"total_events": totalEvents,
	})
	if err != nil {
		err = fmt.Errorf("GetTotalCount: %w", err)
	}
	return t, err
}

// Diff gets the difference in percentage of all paths in this HitList.
//
// e.g. if called with start=2020-01-20; end=2020-01-2020-01-27, then it will
// compare this to start=2020-01-12; end=2020-01-19
//
// The return value is in the same order as paths.
func (h HitLists) Diff(ctx context.Context, rng, prev datetime.Range) ([]float64, error) {
	if len(h) == 0 {
		return nil, nil
	}

	d := -rng.End.Sub(rng.Start)
	prev = datetime.NewRange(rng.Start.Add(d)).To(rng.End.Add(d))

	paths := make([]PathID, 0, len(h))
	for _, hh := range h {
		paths = append(paths, hh.PathID)
	}

	var diffs []float64
	err := database.Select(ctx, &diffs, "load:hit_list.DiffTotal", map[string]any{
		"site":      MustGetSite(ctx).Key,
		"start":     rng.Start,
		"end":       rng.End,
		"prevstart": prev.Start,
		"prevend":   prev.End,
		"paths":     paths,
	})
	if err != nil {
		err = fmt.Errorf("HitList.DiffTotal: %w", err)
	}
	return diffs, err
}
