package goatcounter_test

import (
	"fmt"
	"testing"
	"time"
	"uuid"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func TestDashboardVisitorBreakdowns(t *testing.T) {
	ctx := testenv.DB(t)
	start := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	one, two := uuid.UUID{15: 1}, uuid.UUID{15: 2}
	english, dutch := "eng", "nld"
	testenv.StoreHits(ctx, t, false,
		Hit{CreatedAt: start, Path: "/one?utm_campaign=Spring&utm_source=Newsletter", Session: one, FirstVisit: true, Location: "NL-NB", Language: &english, UserAgentHeader: "Firefox/81.0", Size: []float64{1920, 1080, 1}},
		Hit{CreatedAt: start.Add(time.Minute), Path: "/two", Session: one, FirstVisit: true, Location: "NL-NB", UserAgentHeader: "Firefox/81.0", Size: []float64{1920, 1080, 1}},
		Hit{CreatedAt: start.Add(2 * time.Minute), Path: "signup", Event: true, Session: one, FirstVisit: true, Location: "NL-NB", UserAgentHeader: "Firefox/81.0"},
		Hit{CreatedAt: start.Add(3 * time.Minute), Path: "/one?utm_campaign=Autumn&utm_source=Search", Session: two, FirstVisit: true, Location: "ID-BA", Language: &dutch, UserAgentHeader: "Chrome/77.0", Size: []float64{800, 600, 1}},
	)
	rng := datetime.NewRange(start).To(start.Add(5 * time.Minute))
	metrics, err := GetDashboardMetrics(ctx, rng, PathFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Visits != 2 || metrics.Pageviews != 3 {
		t.Fatalf("metrics = %#v", metrics)
	}
	for _, kind := range []string{"browsers", "systems", "locations", "sizes", "languages", "campaigns", "toprefs"} {
		var stats HitStats
		if err := stats.ListVisitorBreakdown(ctx, kind, "", rng, PathFilter{}, 10, 0); err != nil {
			t.Fatal(err)
		}
		var total int
		for _, row := range stats.Stats {
			total += row.Count
		}
		if total != metrics.Visits {
			t.Errorf("%s total = %d, want %d: %#v", kind, total, metrics.Visits, stats)
		}
	}
	var pages HitLists
	_, _, err = pages.ListVisitorPages(ctx, rng, PathFilter{}, nil, 10, GroupHourly, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || pages[0].Count != 2 || pages[1].Count != 1 {
		t.Fatalf("page visits = %#v", pages)
	}
	var chrome HitStats
	if err := chrome.ListVisitorBreakdown(ctx, "browsers", "Chrome", rng, PathFilter{}, 10, 0); err != nil {
		t.Fatal(err)
	}
	if len(chrome.Stats) != 1 || chrome.Stats[0].Count != 1 {
		t.Fatalf("Chrome versions = %#v", chrome)
	}
	for _, check := range []struct{ kind, detail string }{
		{"locations", "NL"}, {"campaigns", "1"}, {"pagerefs", fmt.Sprint(pages[0].PathID)},
	} {
		var stats HitStats
		if err := stats.ListVisitorBreakdown(ctx, check.kind, check.detail, rng, PathFilter{}, 10, 0); err != nil {
			t.Fatal(err)
		}
		if len(stats.Stats) == 0 {
			t.Errorf("%s detail %q is empty", check.kind, check.detail)
		}
	}
	filter := PathFilterFromIDs([]PathID{pages[1].PathID})
	filteredMetrics, err := GetDashboardMetrics(ctx, rng, filter)
	if err != nil {
		t.Fatal(err)
	}
	var filtered HitStats
	if err := filtered.ListVisitorBreakdown(ctx, "browsers", "", rng, filter, 10, 0); err != nil {
		t.Fatal(err)
	}
	if filteredMetrics.Visits != 1 || len(filtered.Stats) != 1 || filtered.Stats[0].Count != 1 {
		t.Fatalf("filtered visits = %#v, browsers = %#v", filteredMetrics, filtered)
	}
}
