package widgets

import (
	"sync"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zstd/ztime"
)

func TestDashboardWidgetsShareSessionQuery(t *testing.T) {
	ctx, queries := testenv.DBWithQueryFileCounts(t)
	now := time.Now().UTC().Truncate(time.Second)
	testenv.StoreHits(ctx, t, false, goatcounter.Hit{Path: "/one", CreatedAt: now, FirstVisit: true})
	rng := ztime.NewRange(now.Add(-time.Minute)).To(now.Add(time.Minute))
	a := NewArgs(ctx, rng, goatcounter.GroupHourly, nil, 0)
	var totals TotalCount
	var chart TotalPages
	var wg sync.WaitGroup
	for _, w := range []Widget{&totals, &chart} {
		wg.Go(func() {
			if _, err := w.GetData(ctx, a); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if totals.Metrics.Pageviews != 1 || len(chart.Series.Points) == 0 {
		t.Fatalf("unexpected dashboard data: %#v, %#v", totals.Metrics, chart.Series)
	}
	if n := queries.Count("dashboard_metrics.Series"); n != 1 {
		t.Errorf("session series queries = %d, want 1", n)
	}
	if n := queries.Count("dashboard_metrics.Get"); n != 0 {
		t.Errorf("duplicate totals queries = %d, want 0", n)
	}

	// A new request must see hits collected since the first query.
	testenv.StoreHits(ctx, t, false, goatcounter.Hit{Path: "/two", CreatedAt: now, FirstVisit: true})
	a = NewArgs(ctx, rng, goatcounter.GroupHourly, nil, 0)
	if _, err := totals.GetData(ctx, a); err != nil {
		t.Fatal(err)
	}
	if totals.Metrics.Pageviews != 2 || queries.Count("dashboard_metrics.Series") != 2 {
		t.Fatalf("new request reused old metrics: %#v", totals.Metrics)
	}
}
