package cron_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zstd/zjson"
	"zgo.at/zstd/ztest"
	"zgo.at/zstd/ztime"
)

func TestHitStats(t *testing.T) {
	var (
		ctx = testenv.DB(t)
		now = time.Date(2019, 8, 31, 14, 42, 0, 0, time.UTC)
	)

	check := func(wantT, want0 string) {
		t.Helper()

		var stats goatcounter.HitLists
		display, more, err := stats.List(ctx,
			ztime.NewRange(now.Add(-1*time.Hour)).To(now.Add(2*time.Hour)),
			goatcounter.PathFilter{}, nil, 10, goatcounter.GroupHourly)
		if err != nil {
			t.Fatal(err)
		}

		gotT := fmt.Sprintf("%d %t", display, more)
		if wantT != gotT {
			t.Fatalf("wrong totals\nhave: %s\nwant: %s", gotT, wantT)
		}
		if len(stats) != 1 {
			t.Fatalf("len(stats) is not 1: %d", len(stats))
		}

		if d := ztest.Diff(string(zjson.MustMarshal(stats[0])), want0, ztest.DiffJSON); d != "" {
			t.Error("first wrong\n" + d)
		}
	}

	// Store 3 pageviews for one session: two for "/asd" and one for "/zxc", all
	// on the same time.
	testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
		{CreatedAt: now, Path: "/asd", FirstVisit: true},
		{CreatedAt: now, Path: "/asd/"}, // Trailing / should be sanitized and treated identical as /asd
		{CreatedAt: now, Path: "/zxc"},
	}...)

	check("1 false", `{
			"count": 1,
			"path_id":      1,
			"path":         "/asd",
			"event":        false,
			"max":          1,
			"stats": [{
				"day":    "2019-08-31",
				"hourly": [0,0,0,0,0,0,0,0,0,0,0,0,0,0,1,0,0,0,0,0,0,0,0,0],
				"daily":  1,
				"monthly": 1,
				"weekly": 1
			}]}
		`)

	testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
		{CreatedAt: now.Add(2 * time.Hour), Path: "/asd", FirstVisit: true},
		{CreatedAt: now.Add(2 * time.Hour), Path: "/asd"},
	}...)

	// Second hit is excluded because it's stored as:
	//
	// 1        1        2019-08-31 14:00:00  1
	// 1        1        2019-08-31 16:00:00  1
	//
	// And the select has:
	// hour>='2019-08-31 13:42:00' and hour<='2019-08-31 15:42:00'
	//
	// So the second is stored after the end.
	// "now" is 14:42
	// zdb.Dump(ctx, os.Stdout, `select * from hit_counts`)
	check("2 false", `{
			"count":  2,
			"path_id":       1,
			"path":          "/asd",
			"event":         false,
			"max":           1,
			"stats":[{
				"day":     "2019-08-31",
				"hourly":  [0,0,0,0,0,0,0,0,0,0,0,0,0,0,1,0,1,0,0,0,0,0,0,0],
				"daily":   2,
				"monthly": 2,
				"weekly": 2
		}]}`)
}

func TestHitStatsNoCollect(t *testing.T) {
	t.Skip("collection settings were removed")
	ctx := testenv.DB(t)

	site := goatcounter.MustGetSite(ctx)
	site.Settings.Collect ^= goatcounter.CollectSession
	err := site.Update(ctx)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2019, 8, 31, 14, 42, 0, 0, time.UTC)

	testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
		{CreatedAt: now, Path: "/asd"},
		{CreatedAt: now, Path: "/asd"},
		{CreatedAt: now, Path: "/zxc"},
	}...)

	check := func(wantT, want0, want1 string) {
		t.Helper()

		var stats goatcounter.HitLists
		display, more, err := stats.List(ctx,
			ztime.NewRange(now.Add(-1*time.Hour)).To(now.Add(1*time.Hour)),
			goatcounter.PathFilter{}, nil, 10, goatcounter.GroupHourly)
		if err != nil {
			t.Fatal(err)
		}

		gotT := fmt.Sprintf("%d %t", display, more)
		if wantT != gotT {
			t.Fatalf("wrong totals\nhave: %s\nwant: %s", gotT, wantT)
		}
		if len(stats) != 2 {
			t.Fatalf("len(stats) is not 2: %d", len(stats))
		}

		if d := ztest.Diff(string(zjson.MustMarshal(stats[0])), want0, ztest.DiffJSON); d != "" {
			t.Error("first wrong\n" + d)
		}

		if d := ztest.Diff(string(zjson.MustMarshal(stats[1])), want1, ztest.DiffJSON); d != "" {
			t.Error("second wrong\n" + d)
		}
	}

	check("3 false", `{
			"count":         2,
			"path_id":       1,
			"path":          "/asd",
			"event":         false,
			"max":           2,
			"stats":[{
				"day":            "2019-08-31",
				"hourly":  [0,0,0,0,0,0,0,0,0,0,0,0,0,0,2,0,0,0,0,0,0,0,0,0],
				"daily":   2,
				"monthly": 2,
				"weekly": 2
		}]}`,
		`{
			"count":         1,
			"path_id":       2,
			"path":          "/zxc",
			"event":         false,
			"max":           1,
			"stats":[{
				"day":            "2019-08-31",
				"hourly":  [0,0,0,0,0,0,0,0,0,0,0,0,0,0,1,0,0,0,0,0,0,0,0,0],
				"daily":   1,
				"monthly": 1,
				"weekly": 1
		}]}`,
	)
}
