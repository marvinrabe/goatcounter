package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DashboardMetrics contains visit-level totals. A visit (session) ends after
// 30 minutes of inactivity; a visitor is unique per day, as in Plausible.
type DashboardMetrics struct {
	Visitors             int     `db:"visitors"`
	Visits               int     `db:"visits"`
	Pageviews            int     `db:"pageviews"`
	BounceRate           float64 `db:"bounce_rate"`
	VisitDurationSeconds float64 `db:"visit_duration_seconds"`
}

type DashboardMetricPoint struct {
	Day           string  `json:"day"`
	Hour          int     `json:"hour,omitempty"`
	Visitors      float64 `json:"visitors"`
	Visits        float64 `json:"visits"`
	Pageviews     float64 `json:"pageviews"`
	ViewsPerVisit float64 `json:"views_per_visit"`
	BounceRate    float64 `json:"bounce_rate"`
	VisitDuration float64 `json:"visit_duration"`
}

type DashboardMetricSeries struct {
	Group  string                 `json:"group"`
	Points []DashboardMetricPoint `json:"points"`
	// The previous period, aligned point by point with Points.
	Prev []DashboardMetricPoint `json:"prev,omitempty"`
}

// DashboardData contains totals and chart points, for both the selected
// period and the period of the same length right before it.
type DashboardData struct {
	Metrics DashboardMetrics
	Prev    DashboardMetrics
	Series  DashboardMetricSeries
}

type dashboardMetricBucket struct {
	Hour      string  `db:"hour"`
	Visitors  int     `db:"visitors"`
	Visits    int     `db:"visits"`
	Pageviews int     `db:"pageviews"`
	Bounces   int     `db:"bounces"`
	Duration  float64 `db:"duration"`
	// Visits whose bounce and duration are known; the bounce rate and visit
	// duration are relative to these.
	RateVisits int `db:"rate_visits"`
}

func (m DashboardMetrics) ViewsPerVisit() float64 {
	if m.Visits == 0 {
		return 0
	}
	return float64(m.Pageviews) / float64(m.Visits)
}

func (m DashboardMetrics) VisitDuration() time.Duration {
	return time.Duration(m.VisitDurationSeconds * float64(time.Second)).Round(time.Second)
}

// Change returns the relative change from prev to cur in percent, and false
// if there is nothing to compare to.
func Change(cur, prev float64) (float64, bool) {
	if prev == 0 {
		return 0, false
	}
	return (cur - prev) / prev * 100, true
}

// PrevRange is the period of the same length right before rng.
func PrevRange(rng Range) Range {
	d := rng.End.Sub(rng.Start) + time.Second
	return NewRange(rng.Start.Add(-d)).To(rng.End.Add(-d))
}

// params gets the condition for the query's filter, and the parameters for
// the dashboard queries: :site, :start, :end, and the filter's.
func (q Query) params(pathCol, nameCol string) (string, map[string]any) {
	filter, params := q.Filter.SQL(pathCol, nameCol)
	params["site"] = q.Site.Key
	params["start"] = q.Range.Start.Unix()
	params["end"] = q.Range.End.Unix()
	return filter, params
}

// named converts parameters to the named arguments of a query.
func named(params map[string]any) []any {
	args := make([]any, 0, len(params))
	for k, v := range params {
		args = append(args, sql.Named(k, v))
	}
	return args
}

// Hourly buckets are selected in UTC and converted here, so that a period
// with a DST change still has correct days.
func (s *Store) localHour(utcHour string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15", utcHour, time.UTC)
	return t.In(s.Timezone.Loc()), err
}

// Dashboard calculates totals and chart points together. Totals are
// weighted by visits, rather than averaging the per-bucket rates.
func (s *Store) Dashboard(ctx context.Context, q Query, group Period) (DashboardData, error) {
	loc := s.Timezone.Loc()
	group = ChartGroup(q.Range.In(loc), group)

	cur, metrics, err := s.metricBuckets(ctx, q, group)
	if err != nil {
		return DashboardData{}, err
	}
	prevQ := q
	prevQ.Range = PrevRange(q.Range)
	prev, prevMetrics, err := s.metricBuckets(ctx, prevQ, group)
	if err != nil {
		return DashboardData{}, err
	}

	series := DashboardMetricSeries{Group: group.String()}
	series.Points, err = metricPoints(ctx, cur, q.Range.In(loc), group)
	if err != nil {
		return DashboardData{}, err
	}
	series.Prev, err = metricPoints(ctx, prev, prevQ.Range.In(loc), group)
	if err != nil {
		return DashboardData{}, err
	}
	// Only full periods line up; a weekly group can have an extra week.
	series.Prev = series.Prev[:min(len(series.Prev), len(series.Points))]
	return DashboardData{Metrics: metrics, Prev: prevMetrics, Series: series}, nil
}

// metricBuckets loads per-hour buckets in the local timezone, keyed as
// "2006-01-02 15", and the totals for the period.
func (s *Store) metricBuckets(ctx context.Context, q Query, group Period) (map[string]dashboardMetricBucket, DashboardMetrics, error) {
	filter, params := q.params("path", "name")

	// Each visit is counted in the hour of its first pageview. As in
	// Plausible, a custom event (such as an outbound link click) makes a
	// visit not a bounce, and counts for its duration. With a filter only the
	// matching events are considered, as if the others didn't exist.
	var rows []dashboardMetricBucket
	err := s.DB.Select(ctx, &rows, `
		with visits as (
			select
				min(case when name = 'pageview' then ts end) as started,
				sum(name = 'pageview')                       as pageviews,
				sum(name <> 'pageview')                      as custom_events,
				max(ts) - min(ts)                            as duration
			from events
			where site = :site and aggregate = '' and ts >= :start and ts <= :end and `+filter+`
			group by session
			-- Not the alias: events has a pageviews column for migrated rows.
			having sum(name = 'pageview') > 0
		)
		select
			strftime('%Y-%m-%d %H', started, 'unixepoch')      as hour,
			count(*)                                           as visits,
			count(*)                                           as rate_visits,
			sum(pageviews)                                     as pageviews,
			sum(pageviews = 1 and custom_events = 0)           as bounces,
			sum(duration)                                      as duration
		from visits
		group by hour`, named(params)...)
	if err != nil {
		return nil, DashboardMetrics{}, fmt.Errorf("Dashboard: %w", err)
	}

	var total dashboardMetricBucket
	buckets := make(map[string]dashboardMetricBucket)
	add := func(t time.Time, row dashboardMetricBucket) {
		key := StartOf(t, group).Format("2006-01-02 15")
		b := buckets[key]
		b.Visitors += row.Visitors
		b.Visits += row.Visits
		b.Pageviews += row.Pageviews
		b.Bounces += row.Bounces
		b.Duration += row.Duration
		b.RateVisits += row.RateVisits
		buckets[key] = b
		total.Visits += row.Visits
		total.Pageviews += row.Pageviews
		total.Bounces += row.Bounces
		total.Duration += row.Duration
		total.RateVisits += row.RateVisits
	}
	for _, row := range rows {
		t, err := s.localHour(row.Hour)
		if err != nil {
			return nil, DashboardMetrics{}, fmt.Errorf("parse dashboard metric bucket: %w", err)
		}
		add(t, row)
	}

	// A visitor is counted once in every bucket they were active in.
	var visitorHours []struct {
		Hour    string `db:"hour"`
		Visitor int64  `db:"visitor"`
	}
	err = s.DB.Select(ctx, &visitorHours, `
		select distinct strftime('%Y-%m-%d %H', ts, 'unixepoch') as hour, visitor
		from events
		where site = :site and aggregate = '' and ts >= :start and ts <= :end and name = 'pageview' and `+filter, named(params)...)
	if err != nil {
		return nil, DashboardMetrics{}, fmt.Errorf("Dashboard visitors: %w", err)
	}
	type bucketVisitor struct {
		key     string
		visitor int64
	}
	var (
		seen          = make(map[bucketVisitor]struct{}, len(visitorHours))
		seenTotal     = make(map[int64]struct{}, len(visitorHours))
		totalVisitors int
	)
	for _, row := range visitorHours {
		t, err := s.localHour(row.Hour)
		if err != nil {
			return nil, DashboardMetrics{}, fmt.Errorf("parse dashboard metric bucket: %w", err)
		}
		k := bucketVisitor{StartOf(t, group).Format("2006-01-02 15"), row.Visitor}
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			b := buckets[k.key]
			b.Visitors++
			buckets[k.key] = b
		}
		seenTotal[row.Visitor] = struct{}{}
	}
	totalVisitors = len(seenTotal)

	// Migrated Plausible rows are daily totals, at midnight.
	pageFilter, pageParams := q.params("path", "")
	query := `
		select strftime('%Y-%m-%d %H', ts, 'unixepoch') as hour,
			sum(visitors) as visitors, sum(visits) as visits, sum(pageviews) as pageviews,
			sum(bounces) as bounces, sum(visit_duration) as duration, sum(visits) as rate_visits
		from events
		where site = :site and aggregate = 'visitors' and ts >= :start and ts <= :end
		group by ts`
	if !q.Filter.AllPageviews() {
		// With a filter only the per-page totals can be used. Pageviews add
		// up, but a visit to several matching pages is in each of their rows,
		// and Plausible doesn't say which pages were in the same visit. The sum
		// can't be more than the day's visits, though: with a filter that
		// matches every page this gives the exact number, and with a filter
		// that matches one page per visit the sum is exact. The bounce rate
		// and duration aren't known per page, so these visits are left out of
		// them.
		query = `
			with pages as (
				select ts, sum(visitors) as visitors, sum(visits) as visits, sum(pageviews) as pageviews
				from events
				where site = :site and aggregate = 'pages' and ts >= :start and ts <= :end and ` + pageFilter + `
				group by ts
			), days as (
				select ts, sum(visitors) as visitors, sum(visits) as visits
				from events
				where site = :site and aggregate = 'visitors' and ts >= :start and ts <= :end
				group by ts
			)
			select strftime('%Y-%m-%d %H', pages.ts, 'unixepoch') as hour,
				min(pages.visitors, coalesce(days.visitors, pages.visitors)) as visitors,
				min(pages.visits, coalesce(days.visits, pages.visits))       as visits,
				pages.pageviews                                               as pageviews,
				0 as bounces, 0 as duration, 0 as rate_visits
			from pages left join days using (ts)`
	}
	var migrated []dashboardMetricBucket
	err = s.DB.Select(ctx, &migrated, query, named(pageParams)...)
	if err != nil {
		return nil, DashboardMetrics{}, fmt.Errorf("Dashboard migrated: %w", err)
	}
	for _, row := range migrated {
		t, err := s.localHour(row.Hour)
		if err != nil {
			return nil, DashboardMetrics{}, fmt.Errorf("parse migrated bucket: %w", err)
		}
		add(t, row)
		totalVisitors += row.Visitors
	}

	metrics := DashboardMetrics{Visitors: totalVisitors, Visits: total.Visits, Pageviews: total.Pageviews}
	if total.RateVisits > 0 {
		metrics.BounceRate = 100 * float64(total.Bounces) / float64(total.RateVisits)
		metrics.VisitDurationSeconds = total.Duration / float64(total.RateVisits)
	}
	return buckets, metrics, nil
}

func metricPoints(ctx context.Context, buckets map[string]dashboardMetricBucket, rng Range, group Period) ([]DashboardMetricPoint, error) {
	var points []DashboardMetricPoint
	for at := StartOf(rng.Start, group); !at.After(rng.End); at = AddPeriod(at, 1, group) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b := buckets[at.Format("2006-01-02 15")]
		p := DashboardMetricPoint{
			Day:       at.Format("2006-01-02"),
			Visitors:  float64(b.Visitors),
			Visits:    float64(b.Visits),
			Pageviews: float64(b.Pageviews),
		}
		if group == Hour {
			p.Hour = at.Hour()
		}
		if b.Visits > 0 {
			p.ViewsPerVisit = float64(b.Pageviews) / float64(b.Visits)
		}
		if b.RateVisits > 0 {
			p.BounceRate = float64(b.Bounces) / float64(b.RateVisits) * 100
			p.VisitDuration = b.Duration / float64(b.RateVisits)
		}
		points = append(points, p)
	}
	return points, nil
}

// ChartGroup coarsens long ranges without changing their dates. This bounds
// empty chart buckets even while a date input contains a partially typed year.
// Yearly charts have at most 10,000 points for the four-digit years we accept.
func ChartGroup(rng Range, group Period) Period {
	const maxPoints = 2400
	for _, g := range ChartGroups {
		if g >= group && rng.End.Before(AddPeriod(StartOf(rng.Start, g), maxPoints, g)) {
			return g
		}
	}
	return Year
}
