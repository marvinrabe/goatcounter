package goatcounter_test

import (
	"testing"
	"time"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/database"
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

func TestCollectSessions(t *testing.T) {
	ctx := testenv.DB(t)
	at := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)

	testenv.StoreHits(ctx, t,
		hit(at, "10.0.0.1", "/a"),                     // Visit 1
		hit(at.Add(10*time.Minute), "10.0.0.1", "/b"), // Visit 1; 10 minutes
		hit(at.Add(45*time.Minute), "10.0.0.1", "/a"), // Visit 2; 35 minutes since the last pageview
		hit(at.Add(1*time.Minute), "10.0.0.2", "/a"),  // Visit 3; other visitor, bounce
	)
	m, err := GetDashboardData(ctx, day(at), PathFilter{}, GroupDaily)
	if err != nil {
		t.Fatal(err)
	}
	want := DashboardMetrics{Visitors: 2, Visits: 3, Pageviews: 4,
		BounceRate: 200.0 / 3, VisitDurationSeconds: 600.0 / 3}
	if m.Metrics != want {
		t.Errorf("\nhave: %+v\nwant: %+v", m.Metrics, want)
	}

	// Filtered: only pageviews of /a count, as in the unfiltered dashboard.
	m, err = GetDashboardData(ctx, day(at), NewPathFilter("/a at:end"), GroupDaily)
	if err != nil {
		t.Fatal(err)
	}
	if m.Metrics.Visits != 3 || m.Metrics.Pageviews != 3 {
		t.Errorf("filtered: %+v", m.Metrics)
	}
}

func TestCollectAcrossMidnight(t *testing.T) {
	ctx := testenv.DB(t)
	at := time.Date(2026, 6, 10, 23, 50, 0, 0, time.UTC)
	testenv.StoreHits(ctx, t, hit(at, "10.0.0.1", "/a"), hit(at.Add(20*time.Minute), "10.0.0.1", "/b"))

	var sessions, visitors int
	if err := database.Get(ctx, &sessions, `select count(distinct session) from events`); err != nil {
		t.Fatal(err)
	}
	if err := database.Get(ctx, &visitors, `select count(distinct visitor) from events`); err != nil {
		t.Fatal(err)
	}
	// Visitors are daily; the visit continues.
	if sessions != 1 || visitors != 2 {
		t.Errorf("sessions=%d visitors=%d", sessions, visitors)
	}
}

func TestCollectStoresDimensions(t *testing.T) {
	ctx := testenv.DB(t)
	h := hit(time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC), "10.0.0.1",
		"/%C3%BCber-uns/?utm_source=newsletter&utm_medium=email&utm_campaign=A%20%257C%20B&page=2")
	h.Hostname = "WWW.Example.com"
	h.Ref = "https://www.google.de/search?q=x"
	h.Size = Floats{1440}
	h.Language = "deu"
	testenv.StoreHits(ctx, t, h)

	var row struct {
		Hostname, Path, Source, Referrer, UTMSource, UTMMedium, UTMCampaign string
		Browser, BrowserVersion, OS                                         string
		Width                                                               int
		Language                                                            string
	}
	err := database.Get(ctx, &row, `select hostname, path, source, referrer, utm_source as utmsource,
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
	ctx := testenv.DB(t)
	at := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	testenv.StoreHits(ctx, t, hit(at, "10.0.0.1", "/a"), hit(at, "10.0.0.2", "/a"))
	// Migrated Plausible totals, at midnight (UTC in tests).
	err := database.Exec(ctx, `insert into events (site, ts, aggregate, name, browser, visits) values
		('example.com', unixepoch('2026-06-10'), 'browsers', '', 'Firefox', 3),
		('example.com', unixepoch('2026-06-10'), 'browsers', '', 'Safari', 4),
		('example.com', unixepoch('2026-06-11'), 'browsers', '', 'Safari', 100)`)
	if err != nil {
		t.Fatal(err)
	}

	var stats HitStats
	if err := stats.ListVisitorBreakdown(ctx, "browsers", "", day(at), PathFilter{}, 10, 0); err != nil {
		t.Fatal(err)
	}
	if len(stats.Stats) != 2 || stats.Stats[0].Name != "Firefox" || stats.Stats[0].Count != 5 ||
		stats.Stats[1].Name != "Safari" || stats.Stats[1].Count != 4 {
		t.Errorf("%+v", stats.Stats)
	}

	// Migrated data has no per-page dimensions, so it's left out when filtering.
	stats = HitStats{}
	if err := stats.ListVisitorBreakdown(ctx, "browsers", "", day(at), NewPathFilter("/a"), 10, 0); err != nil {
		t.Fatal(err)
	}
	if len(stats.Stats) != 1 || stats.Stats[0].Count != 2 {
		t.Errorf("filtered: %+v", stats.Stats)
	}
}

func TestPathFilter(t *testing.T) {
	ctx := testenv.DB(t)
	at := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	testenv.StoreHits(ctx, t, hit(at, "10.0.0.1", "/blog/one"), hit(at, "10.0.0.2", "/about"),
		hit(at, "10.0.0.3", "/100%_done"))
	for filter, want := range map[string]int{
		"blog": 1, "BLOG": 1, "/about at:start": 1, "about at:start": 0, "blog :not": 2,
		"%": 1, "_": 1, "": 3,
	} {
		var pages HitLists
		_, _, err := pages.ListVisitorPages(ctx, day(at), NewPathFilter(filter), 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(pages) != want {
			t.Errorf("%q: %d pages; want %d", filter, len(pages), want)
		}
	}
}

func TestCollectEvents(t *testing.T) {
	ctx := testenv.DB(t)
	at := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	ev := hit(at.Add(time.Minute), "10.0.0.1", "/a")
	ev.Name, ev.Props = "Outbound Link: Click", `{"url":"https://other.example"}`
	if err := ev.Validate(ctx); err != nil {
		t.Fatal(err)
	}
	testenv.StoreHits(ctx, t, hit(at, "10.0.0.1", "/a"), ev)

	// The event is part of the visit: it's not a bounce, and lasted a minute.
	m, err := GetDashboardData(ctx, day(at), PathFilter{}, GroupDaily)
	if err != nil {
		t.Fatal(err)
	}
	if m.Metrics.Visits != 1 || m.Metrics.Pageviews != 1 || m.Metrics.BounceRate != 0 || m.Metrics.VisitDurationSeconds != 60 {
		t.Errorf("%+v", m.Metrics)
	}
	var stats HitStats
	if err := stats.ListVisitorBreakdown(ctx, "events", "", day(at), PathFilter{}, 10, 0); err != nil {
		t.Fatal(err)
	}
	if len(stats.Stats) != 1 || stats.Stats[0].Name != "Outbound Link: Click" || stats.Stats[0].Count != 1 {
		t.Errorf("%+v", stats.Stats)
	}
	var props string
	if err := database.Get(ctx, &props, `select props from events where name <> 'pageview'`); err != nil || props != `{"url":"https://other.example"}` {
		t.Errorf("props %q: %v", props, err)
	}
}
