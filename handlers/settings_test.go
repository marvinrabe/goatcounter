package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/bgrun"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zdb"
	"zgo.at/zstd/ztest"
	"zgo.at/zstd/ztime"
)

func TestSettingsTpl(t *testing.T) {
	tests := []handlerTest{
		{
			setup: func(ctx context.Context, t *testing.T) {
				now := time.Date(2019, 8, 31, 14, 42, 0, 0, time.UTC)
				testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
					{FirstVisit: true, Path: "/asd", Title: "AAA", CreatedAt: now},
					{FirstVisit: true, Path: "/asd", Title: "AAA", CreatedAt: now},
					{FirstVisit: true, Path: "/zxc", Title: "BBB", CreatedAt: now},
				}...)
			},
			router:   newBackend,
			path:     "/settings/purge?path=/asd",
			auth:     true,
			wantCode: 200,
			wantBody: "<tr><td>2</td><td>/asd</td><td>AAA</td></tr>",
		},
	}

	for _, tt := range tests {
		runTest(t, tt, nil)
	}
}

func TestSettingsPurge(t *testing.T) {
	t.Skip() // Fails after we stopped storing hits.

	tests := []handlerTest{
		{
			setup: func(ctx context.Context, t *testing.T) {
				now := time.Date(2019, 8, 31, 14, 42, 0, 0, time.UTC)
				testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
					{Path: "/asd", CreatedAt: now},
					{Path: "/asd", CreatedAt: now},
					{Path: "/zxc", CreatedAt: now},
				}...)
			},
			router:       newBackend,
			path:         "/settings/purge",
			body:         map[string]string{"path": "/asd", "paths": "1,"},
			method:       "POST",
			auth:         true,
			wantFormCode: 303,
		},
	}

	for _, tt := range tests {
		runTest(t, tt, func(t *testing.T, rr *httptest.ResponseRecorder, r *http.Request) {
			bgrun.Wait("")

			var hits goatcounter.Hits
			err := hits.TestList(r.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) != 1 {
				t.Errorf("%d hits in DB; expected 1:\n%v", len(hits), zdb.DumpString(r.Context(), `select * from hits`))
			}
		})
	}
}

func TestSettingsMerge(t *testing.T) {
	do := func(ctx context.Context, t *testing.T, params map[string]string) {
		form := formBody(params)
		r, rr := newTest(ctx, "POST", "/settings/merge", strings.NewReader(form))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		login(t, r)
		newBackend(ctx).ServeHTTP(rr, r)
		ztest.Code(t, rr, 303)
		bgrun.Wait("")
	}

	check := func(ctx context.Context, t *testing.T, want string) {
		t.Helper()
		have := new(strings.Builder)
		for _, q := range []string{
			`select * from paths         order by path_id`,
			`select * from hit_counts    order by path_id`,
			`select * from browser_stats order by path_id`,
			`select path_id, system_id, day, count, name, version from system_stats
				join systems using(system_id) order by path_id`,
		} {
			zdb.Dump(ctx, have, q)
		}

		if d := ztest.Diff(have.String(), want, ztest.DiffNormalizeWhitespace); d != "" {
			t.Error(d)
		}
	}

	var (
		uaLinux = `Mozilla/5.0 (X11; Linux x86_64; rv:139.0) Gecko/20100101 Firefox/139.0`
		uaMac   = `Mozilla/5.0 (Macintosh; Intel Mac OS X 10.14; rv:139.0) Gecko/20100101 Firefox/139.0`
		uaWin   = `Mozilla/5.0 (Windows NT 10.0; WOW64; rv:139.0) Gecko/20100101 Firefox/139.0`
		now     = ztime.FromString("2025-06-13 12:13:40")
	)

	t.Run("merge one path", func(t *testing.T) {
		ctx := testenv.DB(t)
		testenv.StoreHits(ctx, t, false,
			goatcounter.Hit{FirstVisit: true, CreatedAt: now, Path: "/one", UserAgentHeader: uaLinux},
			goatcounter.Hit{FirstVisit: true, CreatedAt: now, Path: "/two", UserAgentHeader: uaMac},
			goatcounter.Hit{FirstVisit: true, CreatedAt: now, Path: "/three", UserAgentHeader: uaWin})

		do(ctx, t, map[string]string{
			"merge_with": "1",
			"paths":      "2",
		})
		check(ctx, t, `
			path_id  path    title  event
			1        /one           0
			3        /three         0

			path_id  hour                 total
			1        2025-06-13 12:00:00  2
			3        2025-06-13 12:00:00  1

			path_id  browser_id  day                  count
			1        1           2025-06-13 00:00:00  2
			3        1           2025-06-13 00:00:00  1

			path_id  system_id  day                  count  name     version
            1        1          2025-06-13 00:00:00  1      Linux
            1        2          2025-06-13 00:00:00  1      macOS    10.14
            3        3          2025-06-13 00:00:00  1      Windows  10
		`)
	})

	t.Run("merge two paths", func(t *testing.T) {
		ctx := testenv.DB(t)
		testenv.StoreHits(ctx, t, false,
			goatcounter.Hit{FirstVisit: true, CreatedAt: now, Path: "/one", UserAgentHeader: uaLinux},
			goatcounter.Hit{FirstVisit: true, CreatedAt: now, Path: "/two", UserAgentHeader: uaMac},
			goatcounter.Hit{FirstVisit: true, CreatedAt: now, Path: "/three", UserAgentHeader: uaWin})

		do(ctx, t, map[string]string{
			"merge_with": "1",
			"paths":      "2,3",
		})
		check(ctx, t, `
			path_id  path  title  event
			1        /one         0

			path_id  hour                 total
			1        2025-06-13 12:00:00  3

			path_id  browser_id  day                  count
			1        1           2025-06-13 00:00:00  3

			path_id  system_id  day                  count  name     version
            1        1          2025-06-13 00:00:00  1      Linux
            1        2          2025-06-13 00:00:00  1      macOS    10.14
            1        3          2025-06-13 00:00:00  1      Windows  10
		`)
	})
}
