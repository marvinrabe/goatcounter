package goatcounter

import (
	"context"
	"fmt"
	"time"

	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
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
func PrevRange(rng datetime.Range) datetime.Range {
	d := rng.End.Sub(rng.Start) + time.Second
	return datetime.NewRange(rng.Start.Add(-d)).To(rng.End.Add(-d))
}

// rangeParams are the query parameters shared by all dashboard queries.
func rangeParams(ctx context.Context, rng datetime.Range, pathFilter PathFilter, pathCol, nameCol string) map[string]any {
	filter, params := pathFilter.SQL(pathCol, nameCol)
	params["site"] = MustGetSite(ctx).Key
	params["start"] = rng.Start.Unix()
	params["end"] = rng.End.Unix()
	params["filter"] = filter
	return params
}

// Hourly buckets are selected in UTC and converted here, so that a period
// with a DST change still has correct days.
func localHour(ctx context.Context, utcHour string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15", utcHour, time.UTC)
	return t.In(Config(ctx).Timezone.Loc()), err
}

// GetDashboardData calculates totals and chart points together. Totals are
// weighted by visits, rather than averaging the per-bucket rates.
func GetDashboardData(ctx context.Context, rng datetime.Range, pathFilter PathFilter, group Group) (DashboardData, error) {
	loc := Config(ctx).Timezone.Loc()
	group = ChartGroup(rng.In(loc), group)

	cur, metrics, err := metricBuckets(ctx, rng, pathFilter, group)
	if err != nil {
		return DashboardData{}, err
	}
	prevRng := PrevRange(rng)
	prev, prevMetrics, err := metricBuckets(ctx, prevRng, pathFilter, group)
	if err != nil {
		return DashboardData{}, err
	}

	series := DashboardMetricSeries{Group: group.String()}
	series.Points, err = metricPoints(ctx, cur, rng.In(loc), group)
	if err != nil {
		return DashboardData{}, err
	}
	series.Prev, err = metricPoints(ctx, prev, prevRng.In(loc), group)
	if err != nil {
		return DashboardData{}, err
	}
	// Only full periods line up; a weekly group can have an extra week.
	series.Prev = series.Prev[:min(len(series.Prev), len(series.Points))]
	return DashboardData{Metrics: metrics, Prev: prevMetrics, Series: series}, nil
}

// metricBuckets loads per-hour buckets in the local timezone, keyed as
// "2006-01-02 15", and the totals for the period.
func metricBuckets(ctx context.Context, rng datetime.Range, pathFilter PathFilter, group Group) (map[string]dashboardMetricBucket, DashboardMetrics, error) {
	params := rangeParams(ctx, rng, pathFilter, "path", "name")

	// Each visit is counted in the hour of its first pageview. As in
	// Plausible, a custom event (such as an outbound link click) makes a
	// visit not a bounce, and counts for its duration. With a filter only the
	// matching events are considered, as if the others didn't exist.
	var rows []dashboardMetricBucket
	err := database.Select(ctx, &rows, `
		with visits as (
			select
				min(case when name = 'pageview' then ts end) as started,
				sum(name = 'pageview')                       as pageviews,
				sum(name <> 'pageview')                      as custom_events,
				max(ts) - min(ts)                            as duration
			from events
			where site = :site and aggregate = '' and ts >= :start and ts <= :end and :filter
			group by session
			-- Not the alias: events has a pageviews column for migrated rows.
			having sum(name = 'pageview') > 0
		)
		select
			strftime('%Y-%m-%d %H', started, 'unixepoch')      as hour,
			count(*)                                           as visits,
			sum(pageviews)                                     as pageviews,
			sum(pageviews = 1 and custom_events = 0)           as bounces,
			sum(duration)                                      as duration
		from visits
		group by hour`, params)
	if err != nil {
		return nil, DashboardMetrics{}, fmt.Errorf("GetDashboardData: %w", err)
	}

	var total dashboardMetricBucket
	buckets := make(map[string]dashboardMetricBucket)
	add := func(t time.Time, row dashboardMetricBucket) {
		key := metricBucketStart(t, group).Format("2006-01-02 15")
		b := buckets[key]
		b.Visitors += row.Visitors
		b.Visits += row.Visits
		b.Pageviews += row.Pageviews
		b.Bounces += row.Bounces
		b.Duration += row.Duration
		buckets[key] = b
		total.Visits += row.Visits
		total.Pageviews += row.Pageviews
		total.Bounces += row.Bounces
		total.Duration += row.Duration
	}
	for _, row := range rows {
		t, err := localHour(ctx, row.Hour)
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
	err = database.Select(ctx, &visitorHours, `
		select distinct strftime('%Y-%m-%d %H', ts, 'unixepoch') as hour, visitor
		from events
		where site = :site and aggregate = '' and ts >= :start and ts <= :end and name = 'pageview' and :filter`, params)
	if err != nil {
		return nil, DashboardMetrics{}, fmt.Errorf("GetDashboardData visitors: %w", err)
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
		t, err := localHour(ctx, row.Hour)
		if err != nil {
			return nil, DashboardMetrics{}, fmt.Errorf("parse dashboard metric bucket: %w", err)
		}
		k := bucketVisitor{metricBucketStart(t, group).Format("2006-01-02 15"), row.Visitor}
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			b := buckets[k.key]
			b.Visitors++
			buckets[k.key] = b
		}
		seenTotal[row.Visitor] = struct{}{}
	}
	totalVisitors = len(seenTotal)

	// Migrated Plausible rows are daily totals, at midnight. A filtered report
	// can only use the per-page totals; bounce and duration can't be
	// reconstructed by path.
	kind := "visitors"
	if !pathFilter.Empty() {
		kind = "pages"
	}
	iparams := rangeParams(ctx, rng, pathFilter, "path", "")
	iparams["kind"] = kind
	var migrated []dashboardMetricBucket
	err = database.Select(ctx, &migrated, `
		select strftime('%Y-%m-%d %H', ts, 'unixepoch') as hour,
			sum(visitors) as visitors, sum(visits) as visits, sum(pageviews) as pageviews,
			sum(bounces) as bounces, sum(visit_duration) as duration
		from events
		where site = :site and aggregate = :kind and ts >= :start and ts <= :end and :filter
		group by ts`, iparams)
	if err != nil {
		return nil, DashboardMetrics{}, fmt.Errorf("GetDashboardData migrated: %w", err)
	}
	for _, row := range migrated {
		t, err := localHour(ctx, row.Hour)
		if err != nil {
			return nil, DashboardMetrics{}, fmt.Errorf("parse migrated bucket: %w", err)
		}
		add(t, row)
		totalVisitors += row.Visitors
	}

	metrics := DashboardMetrics{Visitors: totalVisitors, Visits: total.Visits, Pageviews: total.Pageviews}
	if total.Visits > 0 {
		metrics.BounceRate = 100 * float64(total.Bounces) / float64(total.Visits)
		metrics.VisitDurationSeconds = total.Duration / float64(total.Visits)
	}
	return buckets, metrics, nil
}

func metricPoints(ctx context.Context, buckets map[string]dashboardMetricBucket, rng datetime.Range, group Group) ([]DashboardMetricPoint, error) {
	var points []DashboardMetricPoint
	for at := metricBucketStart(rng.Start, group); !at.After(rng.End); at = nextMetricBucket(at, group) {
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
		if group.Hourly() {
			p.Hour = at.Hour()
		}
		if b.Visits > 0 {
			p.ViewsPerVisit = float64(b.Pageviews) / float64(b.Visits)
			p.BounceRate = float64(b.Bounces) / float64(b.Visits) * 100
			p.VisitDuration = b.Duration / float64(b.Visits)
		}
		points = append(points, p)
	}
	return points, nil
}

// ChartGroup coarsens long ranges without changing their dates. This bounds
// empty chart buckets even while a date input contains a partially typed year.
// Yearly charts have at most 10,000 points for the four-digit years we accept.
func ChartGroup(rng datetime.Range, group Group) Group {
	const maxPoints = 2400
	for ; group < GroupYearly; group++ {
		start := metricBucketStart(rng.Start, group)
		var limit time.Time
		switch group {
		case GroupHourly:
			limit = start.Add(maxPoints * time.Hour)
		case GroupDaily:
			limit = start.AddDate(0, 0, maxPoints)
		case GroupWeekly:
			limit = start.AddDate(0, 0, maxPoints*7)
		case GroupMonthly:
			limit = start.AddDate(0, maxPoints, 0)
		}
		if rng.End.Before(limit) {
			return group
		}
	}
	return GroupYearly
}

func metricBucketStart(t time.Time, group Group) time.Time {
	if group.Hourly() {
		return t.Truncate(time.Hour)
	}
	if group.Weekly() {
		return datetime.StartOf(t, datetime.Week(false))
	}
	if group.Monthly() {
		return datetime.StartOf(t, datetime.Month)
	}
	if group.Yearly() {
		return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location())
	}
	return datetime.StartOf(t, datetime.Day)
}

func nextMetricBucket(t time.Time, group Group) time.Time {
	if group.Hourly() {
		return t.Add(time.Hour)
	}
	if group.Weekly() {
		return t.AddDate(0, 0, 7)
	}
	if group.Monthly() {
		return t.AddDate(0, 1, 0)
	}
	if group.Yearly() {
		return t.AddDate(1, 0, 0)
	}
	return t.AddDate(0, 0, 1)
}
