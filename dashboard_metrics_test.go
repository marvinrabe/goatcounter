package goatcounter_test

import (
	"testing"
	"time"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zstd/zint"
	"zgo.at/zstd/ztime"
)

func TestDashboardMetrics(t *testing.T) {
	ctx := testenv.DB(t)
	start := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	one := zint.Uint128{1, 1}
	two := zint.Uint128{2, 2}

	testenv.StoreHits(ctx, t, false,
		Hit{CreatedAt: start, Path: "/one", Session: one, FirstVisit: true},
		Hit{CreatedAt: start.Add(2 * time.Minute), Path: "/two", Session: one, FirstVisit: true},
		Hit{CreatedAt: start.Add(3 * time.Minute), Path: "/one", Session: two, FirstVisit: true},
		Hit{CreatedAt: start.Add(4 * time.Minute), Path: "signup", Event: true, Session: two, FirstVisit: true},
	)

	rng := ztime.NewRange(start.Add(-time.Minute)).To(start.Add(10 * time.Minute))
	m, err := GetDashboardMetrics(ctx, rng, PathFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Visits != 2 || m.Pageviews != 3 || m.ViewsPerVisit() != 1.5 || m.BounceRate != 50 || m.VisitDuration() != time.Minute {
		t.Fatalf("unexpected metrics: %#v; views/visit=%v duration=%v", m, m.ViewsPerVisit(), m.VisitDuration())
	}

	var totals HitList
	if err := totals.PageviewTotals(ctx, rng, PathFilter{}, GroupHourly); err != nil {
		t.Fatal(err)
	}
	if totals.Count != 3 || len(totals.Stats) != 1 || totals.Stats[0].Hourly[10] != 3 {
		t.Fatalf("unexpected pageview totals: %#v", totals)
	}

	series, err := GetDashboardMetricSeries(ctx, rng, PathFilter{}, GroupHourly)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Points) != 2 {
		t.Fatalf("unexpected series length: %#v", series)
	}
	p := series.Points[1]
	if p.Visits != 2 || p.Pageviews != 3 || p.ViewsPerVisit != 1.5 || p.BounceRate != 50 || p.VisitDuration != 60 {
		t.Fatalf("unexpected series: %#v", series)
	}
}

func TestDashboardDataWeightedTotals(t *testing.T) {
	ctx := testenv.DB(t)
	start := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	one := zint.Uint128{1, 1}
	testenv.StoreHits(ctx, t, false,
		Hit{CreatedAt: start, Path: "/one", Session: one, FirstVisit: true},
		Hit{CreatedAt: start.Add(2 * time.Hour), Path: "/two", Session: one, FirstVisit: true},
		Hit{CreatedAt: start.Add(24 * time.Hour), Path: "/one", Session: zint.Uint128{2, 2}, FirstVisit: true},
		Hit{CreatedAt: start.Add(25 * time.Hour), Path: "/one", Session: zint.Uint128{3, 3}, FirstVisit: true},
	)
	rng := ztime.NewRange(start).To(start.Add(26 * time.Hour))
	want, err := GetDashboardMetrics(ctx, rng, PathFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range []Group{GroupHourly, GroupDaily, GroupWeekly, GroupMonthly, GroupYearly} {
		data, err := GetDashboardData(ctx, rng, PathFilter{}, group)
		if err != nil {
			t.Fatal(err)
		}
		if data.Metrics != want {
			t.Errorf("%s: totals = %#v, want %#v", group, data.Metrics, want)
		}
		var visits, pageviews float64
		for _, p := range data.Series.Points {
			visits += p.Visits
			pageviews += p.Pageviews
		}
		if visits != 3 || pageviews != 4 {
			t.Errorf("%s: summed series = %v visits, %v pageviews", group, visits, pageviews)
		}
	}
}

func TestDashboardDataLongRange(t *testing.T) {
	ctx := testenv.DB(t)
	testenv.StoreHits(ctx, t, false,
		Hit{CreatedAt: ztime.FromString("2026-09-10"), Path: "/one", Session: zint.Uint128{1, 1}, FirstVisit: true},
		Hit{CreatedAt: ztime.FromString("2026-09-11"), Path: "/one", Session: zint.Uint128{2, 2}, FirstVisit: true},
	)
	rng := ztime.NewRange(ztime.FromString("0000-01-01")).To(ztime.FromString("9999-12-31"))
	data, err := GetDashboardData(ctx, rng, PathFilter{}, GroupHourly)
	if err != nil {
		t.Fatal(err)
	}
	if data.Series.Group != "year" || len(data.Series.Points) != 10000 {
		t.Fatalf("long range was not coarsened: %s, %d points", data.Series.Group, len(data.Series.Points))
	}
	if data.Series.Points[0].Day != "0000-01-01" || data.Series.Points[9999].Day != "9999-01-01" {
		t.Fatal("chart no longer covers the entire selected range")
	}
	var visits, pageviews float64
	for _, p := range data.Series.Points {
		visits += p.Visits
		pageviews += p.Pageviews
	}
	if visits != 2 || pageviews != 2 || data.Metrics.Visits != 2 || data.Metrics.Pageviews != 2 || data.Series.Points[2026].Visits != 2 {
		t.Fatalf("coarsening lost data: totals=%#v, visits=%v, pageviews=%v", data.Metrics, visits, pageviews)
	}
}
