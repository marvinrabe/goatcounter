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
