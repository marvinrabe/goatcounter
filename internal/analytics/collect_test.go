package analytics_test

import (
	"context"
	"strings"
	"testing"
	"time"

	. "github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

const firefox = "Mozilla/5.0 (X11; Linux x86_64; rv:72.0) Gecko/20100101 Firefox/72.0"

func hit(at time.Time, ip, path string) Hit {
	return Hit{CreatedAt: at, RemoteAddr: ip, UserAgentHeader: firefox, Path: path}
}

func day(t time.Time) datetime.Range {
	return datetime.NewRange(t.Truncate(24 * time.Hour)).To(t.Truncate(24 * time.Hour).Add(24*time.Hour - time.Second))
}

func query(store *Store, rng datetime.Range, filter PathFilter) Query {
	return Query{Site: store.Sites[0], Range: rng, Filter: filter}
}

func TestCollectSessions(t *testing.T) {
	store, ctx := testenv.Store(t), context.Background()
	at := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)

	testenv.StoreHits(t, store,
		hit(at, "10.0.0.1", "/a"),                     // Visit 1
		hit(at.Add(10*time.Minute), "10.0.0.1", "/b"), // Visit 1; 10 minutes
		hit(at.Add(45*time.Minute), "10.0.0.1", "/a"), // Visit 2; 35 minutes since the last pageview
		hit(at.Add(1*time.Minute), "10.0.0.2", "/a"),  // Visit 3; other visitor, bounce
	)
	m, err := store.Dashboard(ctx, query(store, day(at), PathFilter{}), GroupDaily)
	if err != nil {
		t.Fatal(err)
	}
	want := DashboardMetrics{Visitors: 2, Visits: 3, Pageviews: 4,
		BounceRate: 200.0 / 3, VisitDurationSeconds: 600.0 / 3}
	if m.Metrics != want {
		t.Errorf("\nhave: %+v\nwant: %+v", m.Metrics, want)
	}

	// Filtered: only pageviews of /a count, as in the unfiltered dashboard.
	m, err = store.Dashboard(ctx, query(store, day(at), NewPathFilter("/a at:end")), GroupDaily)
	if err != nil {
		t.Fatal(err)
	}
	if m.Metrics.Visits != 3 || m.Metrics.Pageviews != 3 {
		t.Errorf("filtered: %+v", m.Metrics)
	}
}

func TestCollectAcrossMidnight(t *testing.T) {
	store, ctx := testenv.Store(t), context.Background()
	at := time.Date(2026, 6, 10, 23, 50, 0, 0, time.UTC)
	testenv.StoreHits(t, store, hit(at, "10.0.0.1", "/a"), hit(at.Add(20*time.Minute), "10.0.0.1", "/b"))

	var sessions, visitors int
	if err := store.DB.Get(ctx, &sessions, `select count(distinct session) from events`); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Get(ctx, &visitors, `select count(distinct visitor) from events`); err != nil {
		t.Fatal(err)
	}
	// Visitors are daily; the visit continues.
	if sessions != 1 || visitors != 2 {
		t.Errorf("sessions=%d visitors=%d", sessions, visitors)
	}
}

func TestCollectStoresDimensions(t *testing.T) {
	store, ctx := testenv.Store(t), context.Background()
	h := hit(time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC), "10.0.0.1",
		"/%C3%BCber-uns/?utm_source=newsletter&utm_medium=email&utm_campaign=A%20%257C%20B&page=2")
	h.Hostname = "WWW.Example.com"
	h.Ref = "https://www.google.de/search?q=x"
	h.Width = 1440
	h.Language = "deu"
	testenv.StoreHits(t, store, h)

	var row struct {
		Hostname, Path, Source, Referrer, UTMSource, UTMMedium, UTMCampaign string
		Browser, BrowserVersion, OS                                         string
		Width                                                               int
		Language                                                            string
	}
	err := store.DB.Get(ctx, &row, `select hostname, path, source, referrer, utm_source as utmsource,
		utm_medium as utmmedium, utm_campaign as utmcampaign, browser,
		browser_version as browserversion, os, width, language from events`)
	if err != nil {
		t.Fatal(err)
	}
	want := row
	// Stored as Plausible does: the decoded path as sent without query, and
	// query parameters decoded once.
	want.Hostname, want.Path, want.Source, want.Referrer = "example.com", "/über-uns/", "newsletter", "google.de/search"
	want.UTMSource, want.UTMMedium, want.UTMCampaign = "newsletter", "email", "A %7C B"
	want.Browser, want.BrowserVersion, want.OS, want.Width, want.Language = "Firefox", "72.0", "GNU/Linux", 1440, "deu"
	if row != want {
		t.Errorf("\nhave: %+v\nwant: %+v", row, want)
	}
}

func TestBreakdownMergesImported(t *testing.T) {
	store, ctx := testenv.Store(t), context.Background()
	at := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	testenv.StoreHits(t, store, hit(at, "10.0.0.1", "/a"), hit(at, "10.0.0.2", "/a"))
	// Migrated Plausible totals, at midnight (UTC in tests).
	err := store.DB.Exec(ctx, `insert into events (site, ts, aggregate, name, browser, visits) values
		('example.com', unixepoch('2026-06-10'), 'browsers', '', 'Firefox', 3),
		('example.com', unixepoch('2026-06-10'), 'browsers', '', 'Safari', 4),
		('example.com', unixepoch('2026-06-11'), 'browsers', '', 'Safari', 100)`)
	if err != nil {
		t.Fatal(err)
	}

	stats, err := store.Breakdown(ctx, query(store, day(at), PathFilter{}), "browsers", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Stats) != 2 || stats.Stats[0].Name != "Firefox" || stats.Stats[0].Count != 5 ||
		stats.Stats[1].Name != "Safari" || stats.Stats[1].Count != 4 {
		t.Errorf("%+v", stats.Stats)
	}

	// Migrated data has no per-page dimensions, so it's left out when filtering.
	stats, err = store.Breakdown(ctx, query(store, day(at), NewPathFilter("/a")), "browsers", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Stats) != 1 || stats.Stats[0].Count != 2 {
		t.Errorf("filtered: %+v", stats.Stats)
	}
}

func TestPathFilter(t *testing.T) {
	store, ctx := testenv.Store(t), context.Background()
	at := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	testenv.StoreHits(t, store, hit(at, "10.0.0.1", "/blog/one"), hit(at, "10.0.0.2", "/about"),
		hit(at, "10.0.0.3", "/100%_done"))
	for filter, want := range map[string]int{
		"blog": 1, "BLOG": 1, "/about at:start": 1, "about at:start": 0, "blog :not": 2,
		"%": 1, "_": 1, "": 3,
	} {
		pages, _, err := store.Pages(ctx, query(store, day(at), NewPathFilter(filter)), 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(pages) != want {
			t.Errorf("%q: %d pages; want %d", filter, len(pages), want)
		}
	}
}

func TestCollectEvents(t *testing.T) {
	store, ctx := testenv.Store(t), context.Background()
	at := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	ev := hit(at.Add(time.Minute), "10.0.0.1", "/a")
	ev.Name, ev.Props = "Outbound Link: Click", `{"url":"https://other.example"}`
	if err := ev.Validate(); err != nil {
		t.Fatal(err)
	}
	testenv.StoreHits(t, store, hit(at, "10.0.0.1", "/a"), ev)

	// The event is part of the visit: it's not a bounce, and lasted a minute.
	m, err := store.Dashboard(ctx, query(store, day(at), PathFilter{}), GroupDaily)
	if err != nil {
		t.Fatal(err)
	}
	if m.Metrics.Visits != 1 || m.Metrics.Pageviews != 1 || m.Metrics.BounceRate != 0 || m.Metrics.VisitDurationSeconds != 60 {
		t.Errorf("%+v", m.Metrics)
	}
	stats, err := store.Breakdown(ctx, query(store, day(at), PathFilter{}), "events", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Stats) != 1 || stats.Stats[0].Name != "Outbound Link: Click" || stats.Stats[0].Count != 1 {
		t.Errorf("%+v", stats.Stats)
	}
	var props string
	if err := store.DB.Get(ctx, &props, `select props from events where name <> 'pageview'`); err != nil || props != `{"url":"https://other.example"}` {
		t.Errorf("props %q: %v", props, err)
	}
}

func TestFilteredMigratedTotals(t *testing.T) {
	store, ctx := testenv.Store(t), context.Background()
	at := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	// One live bounce on /a.
	testenv.StoreHits(t, store, hit(at, "10.0.0.1", "/a"))
	// A migrated day with 4 visits, 3 of which saw /a and 2 /b: some saw both.
	err := store.DB.Exec(ctx, `insert into events (site, ts, aggregate, name, path, visitors, visits, pageviews, bounces, visit_duration) values
		('example.com', unixepoch('2026-06-10'), 'visitors', '', '', 4, 4, 9, 1, 400),
		('example.com', unixepoch('2026-06-10'), 'pages', '', '/a', 3, 3, 5, 0, 0),
		('example.com', unixepoch('2026-06-10'), 'pages', '', '/b', 2, 2, 4, 0, 0)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		filter            string
		visits, pageviews int
		bounceRate        float64
	}{
		{"", 1 + 4, 1 + 9, 100 * 2.0 / 5},
		// Matches every pageview, so the same as no filter.
		{"is:pageview", 1 + 4, 1 + 9, 100 * 2.0 / 5},
		// The 3 + 2 page visits are capped at the day's 4 visits; the bounce
		// rate only has the live visit, as it's unknown per page.
		{"/", 1 + 4, 1 + 9, 100},
		{"/a", 1 + 3, 1 + 5, 100},
		{"/b", 2, 4, 0},
	} {
		m, err := store.Dashboard(ctx, query(store, day(at), NewPathFilter(tt.filter)), GroupDaily)
		if err != nil {
			t.Fatal(err)
		}
		if m.Metrics.Visits != tt.visits || m.Metrics.Pageviews != tt.pageviews || m.Metrics.BounceRate != tt.bounceRate {
			t.Errorf("filter %q: %+v; want visits=%d pageviews=%d bounce=%v",
				tt.filter, m.Metrics, tt.visits, tt.pageviews, tt.bounceRate)
		}
	}
}

func TestHitValidate(t *testing.T) {
	h := Hit{Name: strings.Repeat("x", 121), Width: -1, Props: "[]"}
	err := h.Validate()
	want := "path: must be set; name: must be at most 120 characters; width: must be between 0 and 100000; props: must be a JSON object with string values"
	if err == nil || err.Error() != want {
		t.Errorf("\nhave: %v\nwant: %s", err, want)
	}
}
