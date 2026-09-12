package goatcounter

import (
	"context"
	"fmt"
	"time"

	"zgo.at/errors"
	"zgo.at/zdb"
	"zgo.at/zstd/ztime"
)

// DashboardMetrics contains session-level metrics that can be calculated from
// the raw hits table. A session is a visit; GoatCounter intentionally does not
// keep a durable visitor identifier, so unique visitors are not available.
type DashboardMetrics struct {
	Visits               int     `db:"visits"`
	Pageviews            int     `db:"pageviews"`
	BounceRate           float64 `db:"bounce_rate"`
	VisitDurationSeconds float64 `db:"visit_duration_seconds"`
}

type DashboardMetricPoint struct {
	Day           string  `json:"day"`
	Hour          int     `json:"hour,omitempty"`
	Visits        float64 `json:"visits"`
	Pageviews     float64 `json:"pageviews"`
	ViewsPerVisit float64 `json:"views_per_visit"`
	BounceRate    float64 `json:"bounce_rate"`
	VisitDuration float64 `json:"visit_duration"`
}

type DashboardMetricSeries struct {
	Group  string                 `json:"group"`
	Points []DashboardMetricPoint `json:"points"`
}

// DashboardData contains totals and chart points from a single session query.
type DashboardData struct {
	Metrics DashboardMetrics
	Series  DashboardMetricSeries
}

type dashboardMetricBucket struct {
	Hour      string  `db:"hour"`
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

// GetDashboardMetrics calculates visit and pageview metrics for a period. Only
// pageviews are included: custom events are not visits and cannot be bounces.
func GetDashboardMetrics(ctx context.Context, rng ztime.Range, pathFilter PathFilter) (DashboardMetrics, error) {
	filterSQL, filterParams := pathFilter.SQL(ctx, "hits")
	var m DashboardMetrics
	err := zdb.Get(ctx, &m, "load:dashboard_metrics.Get", filterParams, map[string]any{
		"start":  rng.Start,
		"end":    rng.End,
		"filter": filterSQL,
		"sqlite": zdb.SQLDialect(ctx) == zdb.DialectSQLite,
	})
	return m, errors.Wrap(err, "GetDashboardMetrics")
}

// GetDashboardMetricSeries returns the five available metrics using the same
// buckets as the dashboard's current grouping control.
func GetDashboardMetricSeries(
	ctx context.Context, rng ztime.Range, pathFilter PathFilter, group Group,
) (DashboardMetricSeries, error) {
	data, err := GetDashboardData(ctx, rng, pathFilter, group)
	return data.Series, err
}

// GetDashboardData calculates totals and chart points together. Totals are
// weighted by visits, rather than averaging the per-bucket rates.
func GetDashboardData(ctx context.Context, rng ztime.Range, pathFilter PathFilter, group Group) (DashboardData, error) {
	loc := Config(ctx).Timezone.Loc()
	group = ChartGroup(rng.In(loc), group)
	filterSQL, filterParams := pathFilter.SQL(ctx, "hits")
	var rows []dashboardMetricBucket
	err := zdb.Select(ctx, &rows, "load:dashboard_metrics.Series", filterParams, map[string]any{
		"start":   rng.Start,
		"end":     rng.End,
		"filter":  filterSQL,
		"offset":  Config(ctx).Timezone.Offset(),
		"offset2": fmt.Sprintf("%d minutes", Config(ctx).Timezone.Offset()),
		"sqlite":  zdb.SQLDialect(ctx) == zdb.DialectSQLite,
	})
	if err != nil {
		return DashboardData{}, errors.Wrap(err, "GetDashboardData")
	}

	start := metricBucketStart(rng.Start.In(loc), group)
	end := rng.End.In(loc)
	buckets := make(map[string]dashboardMetricBucket)
	var total dashboardMetricBucket
	for _, row := range rows {
		total.Visits += row.Visits
		total.Pageviews += row.Pageviews
		total.Bounces += row.Bounces
		total.Duration += row.Duration
		t, err := time.ParseInLocation("2006-01-02 15", row.Hour, loc)
		if err != nil {
			return DashboardData{}, errors.Wrap(err, "parse dashboard metric bucket")
		}
		key := metricBucketStart(t, group).Format("2006-01-02 15")
		b := buckets[key]
		b.Visits += row.Visits
		b.Pageviews += row.Pageviews
		b.Bounces += row.Bounces
		b.Duration += row.Duration
		buckets[key] = b
	}

	series := DashboardMetricSeries{Group: group.String()}
	for at := start; !at.After(end); at = nextMetricBucket(at, group) {
		if err := ctx.Err(); err != nil {
			return DashboardData{}, err
		}
		b := buckets[at.Format("2006-01-02 15")]
		p := DashboardMetricPoint{
			Day:       at.Format("2006-01-02"),
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
		series.Points = append(series.Points, p)
	}
	metrics := DashboardMetrics{Visits: total.Visits, Pageviews: total.Pageviews}
	if total.Visits > 0 {
		metrics.BounceRate = 100 * float64(total.Bounces) / float64(total.Visits)
		metrics.VisitDurationSeconds = total.Duration / float64(total.Visits)
	}
	return DashboardData{Metrics: metrics, Series: series}, nil
}

// ChartGroup coarsens long ranges without changing their dates. This bounds
// empty chart buckets even while a date input contains a partially typed year.
// Yearly charts have at most 10,000 points for the four-digit years we accept.
func ChartGroup(rng ztime.Range, group Group) Group {
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
		return ztime.StartOf(t, ztime.Week(false))
	}
	if group.Monthly() {
		return ztime.StartOf(t, ztime.Month)
	}
	if group.Yearly() {
		return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location())
	}
	return ztime.StartOf(t, ztime.Day)
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

// PageviewTotals returns the raw pageview time series used by the main chart.
// This differs from Totals, which counts a path at most once per session.
func (h *HitList) PageviewTotals(ctx context.Context, rng ztime.Range, pathFilter PathFilter, group Group) error {
	filterSQL, filterParams := pathFilter.SQL(ctx, "hits")
	err := zdb.Get(ctx, &h.Stats2, "load:dashboard_metrics.Pageviews", filterParams, map[string]any{
		"start":   rng.Start,
		"end":     rng.End,
		"filter":  filterSQL,
		"offset":  Config(ctx).Timezone.Offset(),
		"offset2": fmt.Sprintf("%d minutes", Config(ctx).Timezone.Offset()),
		"sqlite":  zdb.SQLDialect(ctx) == zdb.DialectSQLite,
	})
	if err != nil {
		return errors.Wrap(err, "HitList.PageviewTotals")
	}

	h.Count, h.Path = h.sum(ctx, rng, group), PathTotals
	h.Max = max(h.Max, 10)
	return nil
}
