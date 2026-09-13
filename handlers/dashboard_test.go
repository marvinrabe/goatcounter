package handlers

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zdb"
	"zgo.at/zstd/ztime"
)

func TestDashboard(t *testing.T) {
	tests := []handlerTest{
		{
			name:     "no-data",
			router:   newBackend,
			auth:     true,
			wantCode: 200,
			wantBody: `id="tracking-code"`,
		},
	}

	for _, tt := range tests {
		runTest(t, tt, func(t *testing.T, rr *httptest.ResponseRecorder, _ *http.Request) {
			if strings.Contains(rr.Body.String(), `id="usermenu"`) {
				t.Error("dashboard-only main menu is still rendered")
			}
			if strings.Contains(rr.Body.String(), "No data received") || !strings.Contains(rr.Body.String(), `id="dash-widgets"`) {
				t.Error("empty sites should display the regular dashboard")
			}
			for _, input := range regexp.MustCompile(`<input[^>]+name="period-(?:start|end)"[^>]*>`).FindAllString(rr.Body.String(), -1) {
				if strings.Contains(input, "min=") || strings.Contains(input, "max=") {
					t.Errorf("date input still restricts available dates: %s", input)
				}
			}
		})
	}
}

func TestDashboardTrackingCode(t *testing.T) {
	ctx := testenv.DB(t)
	second := goatcounter.Site{Key: "second.example", LinkDomain: "second.example"}
	second.Defaults()
	goatcounter.Config(ctx).Sites = append(goatcounter.Config(ctx).Sites, second)
	goatcounter.Config(ctx).BasePath = "/stats"
	router := NewBackend(zdb.MustGetDB(ctx), true, "", "/stats", 10, Ratelimits{}, "", Auth{Mode: AuthPublic})
	for _, name := range []string{"example.com", "second.example"} {
		r, rr := newTest(ctx, http.MethodGet, "/stats/?site="+name, nil)
		r.Host = "analytics.example"
		router.ServeHTTP(rr, r)
		if rr.Code != http.StatusOK {
			t.Fatalf("dashboard: %d, %s", rr.Code, rr.Body.String())
		}
		match := regexp.MustCompile(`(?s)<textarea[^>]+id="tracking-snippet"[^>]*>(.*?)</textarea>`).FindStringSubmatch(rr.Body.String())
		want := "<script data-site=\"" + name + "\"\n        async src=\"//analytics.example/stats/count.js\"></script>"
		if len(match) != 2 || html.UnescapeString(match[1]) != want {
			t.Fatalf("wrong tracking snippet for %s: %v", name, match)
		}
	}
}

func TestGetPeriodWithoutSiteMetadata(t *testing.T) {
	ctx := testenv.Context(nil)
	for _, tt := range []struct {
		start, end string
		wantErr    bool
	}{
		{"2000-01-01", "2000-01-31", false},
		{"2100-01-01", "2100-01-31", false},
		{"2000-01-01", "2100-01-31", false},
		{"2000-01-01", "2000-01-01", false},
		{"0001-01-01", "9999-12-31", false},
		{"0001-01-01", "0000-12-31", true},
		{"2000-01-02", "2000-01-01", true},
		{"not-a-date", "2000-01-01", true},
	} {
		r := httptest.NewRequest(http.MethodGet, "/?period-start="+tt.start+"&period-end="+tt.end, nil).WithContext(ctx)
		rng, err := getPeriod(r)
		if (err != nil) != tt.wantErr {
			t.Fatalf("%s–%s: %v", tt.start, tt.end, err)
		}
		if !tt.wantErr && (rng.Start.Format("2006-01-02") != tt.start || rng.End.Format("2006-01-02") != tt.end) {
			t.Errorf("%s–%s was changed to %s–%s", tt.start, tt.end, rng.Start, rng.End)
		}
	}
}

func TestDashboardPastAndFuture(t *testing.T) {
	for _, date := range []string{"2000-01-01", "2100-01-01"} {
		runTest(t, handlerTest{
			name: date, router: newBackend, auth: true,
			path:     "/?period-start=" + date + "&period-end=" + date,
			wantCode: http.StatusOK, wantBody: `value="` + date + `"`,
		}, nil)
	}
}

func TestDashboardLongRange(t *testing.T) {
	ctx := testenv.DB(t)
	testenv.StoreHits(ctx, t, false, goatcounter.Hit{
		Path: "/sample-page", CreatedAt: ztime.FromString("2026-09-10"), FirstVisit: true,
	})
	router := NewBackend(zdb.MustGetDB(ctx), true, "", "", 10, Ratelimits{}, "", Auth{Mode: AuthPublic})
	for _, tt := range []struct{ start, end, group string }{
		{"2020-01-01", "2026-12-31", "week"},
		{"1900-01-01", "2100-12-31", "year"},
		{"0001-01-01", "9999-12-31", "year"},
	} {
		r, rr := newTest(ctx, http.MethodGet, "/?period-start="+tt.start+"&period-end="+tt.end+"&group=day", nil)
		router.ServeHTTP(rr, r)
		body := rr.Body.String()
		if rr.Code != http.StatusOK || !strings.Contains(body, "/sample-page") || !strings.Contains(body, `data-group="`+tt.group+`"`) {
			t.Fatalf("%s–%s: status %d, expected chart and sample page", tt.start, tt.end, rr.Code)
		}
		if !strings.Contains(body, `value="`+tt.start+`"`) || !strings.Contains(body, `value="`+tt.end+`"`) {
			t.Fatal("selected dates changed")
		}
		if len(body) > 2_000_000 {
			t.Fatalf("unbounded dashboard response: %d bytes", len(body))
		}
	}
}

func TestGetGroup(t *testing.T) {
	tests := []struct {
		days      int
		saved     goatcounter.Group
		query     string
		wantGroup goatcounter.Group
		wantAllow goatcounter.Groups
	}{
		{3, goatcounter.GroupHourly, "", goatcounter.GroupHourly,
			goatcounter.Groups{goatcounter.GroupHourly}},
		{3, goatcounter.GroupDaily, "", goatcounter.GroupHourly,
			goatcounter.Groups{goatcounter.GroupHourly}},

		{30, goatcounter.GroupHourly, "", goatcounter.GroupHourly,
			goatcounter.Groups{goatcounter.GroupHourly, goatcounter.GroupDaily}},
		{30, goatcounter.GroupDaily, "", goatcounter.GroupDaily,
			goatcounter.Groups{goatcounter.GroupHourly, goatcounter.GroupDaily}},
		{30, goatcounter.GroupWeekly, "", goatcounter.GroupHourly,
			goatcounter.Groups{goatcounter.GroupHourly, goatcounter.GroupDaily}},
		{30, goatcounter.GroupDaily, "hour", goatcounter.GroupHourly,
			goatcounter.Groups{goatcounter.GroupHourly, goatcounter.GroupDaily}},

		{120, goatcounter.GroupHourly, "", goatcounter.GroupDaily,
			goatcounter.Groups{goatcounter.GroupDaily, goatcounter.GroupWeekly}},
		{120, goatcounter.GroupWeekly, "", goatcounter.GroupWeekly,
			goatcounter.Groups{goatcounter.GroupDaily, goatcounter.GroupWeekly}},

		{365, goatcounter.GroupMonthly, "", goatcounter.GroupMonthly,
			goatcounter.Groups{goatcounter.GroupDaily, goatcounter.GroupWeekly, goatcounter.GroupMonthly}},
		{3650, goatcounter.GroupDaily, "day", goatcounter.GroupWeekly,
			goatcounter.Groups{goatcounter.GroupWeekly, goatcounter.GroupMonthly}},
		{36500, goatcounter.GroupHourly, "day", goatcounter.GroupMonthly,
			goatcounter.Groups{goatcounter.GroupMonthly}},
		{365000, goatcounter.GroupMonthly, "month", goatcounter.GroupYearly,
			goatcounter.Groups{goatcounter.GroupYearly}},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			start := ztime.FromString("2020-06-18")
			rng := ztime.NewRange(start).To(start.AddDate(0, 0, tt.days).Add(24*time.Hour - time.Second))

			r := httptest.NewRequest("GET", "/?group="+tt.query, nil)
			group, allow := getGroup(r, tt.saved, rng)
			if group != tt.wantGroup || !slices.Equal(allow, tt.wantAllow) {
				t.Errorf("\nhave: %s, %s\nwant: %s, %s", group, allow, tt.wantGroup, tt.wantAllow)
			}
		})
	}
}

func TestTimeRange(t *testing.T) {
	tests := []struct {
		rng, now, wantStart, wantEnd string
	}{
		{"week", "2020-12-02",
			"2020-11-25 00:00:00", "2020-12-02 23:59:59"},
		{"month", "2020-01-18",
			"2019-12-18 00:00:00", "2020-01-18 23:59:59"},
		{"quarter", "2020-01-18",
			"2019-10-18 00:00:00", "2020-01-18 23:59:59"},
		{"half-year", "2020-01-18",
			"2019-07-18 00:00:00", "2020-01-18 23:59:59"},
		{"year", "2020-01-18",
			"2019-01-18 00:00:00", "2020-01-18 23:59:59"},

		{"0", "2020-06-18",
			"2020-06-18 00:00:00", "2020-06-18 23:59:59"},
		{"1", "2020-06-18",
			"2020-06-17 00:00:00", "2020-06-18 23:59:59"},
		{"42", "2020-06-18",
			"2020-05-07 00:00:00", "2020-06-18 23:59:59"},
	}

	for _, tt := range tests {
		t.Run(tt.rng+"-"+tt.now, func(t *testing.T) {
			t.Run("UTC", func(t *testing.T) {
				ctx := ztime.WithNow(context.Background(), ztime.FromString(tt.now))
				rng := timeRange(ctx, tt.rng, time.UTC, false)
				gotStart := rng.Start.Format("2006-01-02 15:04:05")
				gotEnd := rng.End.Format("2006-01-02 15:04:05")

				if gotStart != tt.wantStart || gotEnd != tt.wantEnd {
					t.Errorf("\nhave: %q, %q\nwant: %q, %q",
						gotStart, gotEnd, tt.wantStart, tt.wantEnd)
				}
			})

			// t.Run("Asia/Makassar", func(t *testing.T) {
			// })
		})
	}
}
