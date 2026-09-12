package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/isbot"
	"zgo.at/zdb"
	"zgo.at/zstd/zcrypto"
	"zgo.at/zstd/zint"
	"zgo.at/zstd/zjson"
	"zgo.at/zstd/ztest"
	"zgo.at/zstd/ztime"
)

func TestBackendCountLanguage(t *testing.T) {
	tests := []struct {
		name    string
		collect bool
		header  string
		want    string
	}{
		{"always collected", false, "nl-BE,nl;q=0.9", "nld"},
		{"collected", true, "nl-BE,nl;q=0.9", "nld"},
		{"no header", true, "", ""},
		{"unknown language", true, "xx", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testenv.DB(t)
			ctx = ztime.WithNow(ctx, ztime.FromString("2019-06-18 14:42:00"))

			site := Site(ctx)
			site.Settings.Collect.Set(goatcounter.CollectHits)
			if tt.collect {
				site.Settings.Collect.Set(goatcounter.CollectLanguage)
			}
			if err := site.Update(ctx); err != nil {
				t.Fatal(err)
			}

			r, rr := newTest(ctx, "GET", "/count?p=/foo.html", nil)
			if tt.header != "" {
				r.Header.Set("Accept-Language", tt.header)
			}
			login(t, r)
			newBackend(ctx).ServeHTTP(rr, r)
			ztest.Code(t, rr, 200)

			if _, err := goatcounter.Memstore.Persist(ctx); err != nil {
				t.Fatal(err)
			}

			var hits goatcounter.Hits
			if err := hits.TestList(ctx); err != nil {
				t.Fatal(err)
			}
			if len(hits) != 1 {
				t.Fatalf("len(hits) = %d: %#v", len(hits), hits)
			}

			have := ""
			if l := hits[0].Language; l != nil {
				have = *l
			}
			if have != tt.want {
				t.Errorf("\nhave: %q\nwant: %q", have, tt.want)
			}
		})
	}
}

func TestBackendCount(t *testing.T) {
	tests := []struct {
		name     string
		query    url.Values
		set      func(r *http.Request)
		wantCode int
		hit      goatcounter.Hit
	}{
		{"no path", url.Values{}, nil, 400, goatcounter.Hit{}},
		{"invalid size", url.Values{"p": {"/x"}, "s": {"xxx"}}, nil, 400, goatcounter.Hit{}},

		{"only path", url.Values{"p": {"/foo.html"}}, nil, 200, goatcounter.Hit{
			Path: "/foo.html",
		}},

		{"add slash", url.Values{"p": {"foo.html"}}, nil, 200, goatcounter.Hit{
			Path: "/foo.html",
		}},

		{"event", url.Values{"p": {"foo.html"}, "e": {"true"}}, nil, 200, goatcounter.Hit{
			Path:  "foo.html",
			Event: true,
		}},

		{"params", url.Values{"p": {"/foo.html?a=b&c=d"}}, nil, 200, goatcounter.Hit{
			Path: "/foo.html?a=b&c=d",
		}},

		{"ref", url.Values{"p": {"/foo.html"}, "r": {"https://example.com"}}, nil, 200, goatcounter.Hit{
			Path:      "/foo.html",
			Ref:       "example.com",
			RefScheme: "h",
		}},

		{"str ref", url.Values{"p": {"/foo.html"}, "r": {"example"}}, nil, 200, goatcounter.Hit{
			Path:      "/foo.html",
			Ref:       "example",
			RefScheme: "o",
		}},

		{"ref params", url.Values{"p": {"/foo.html"}, "r": {"https://example.com?p=x"}}, nil, 200, goatcounter.Hit{
			Path:      "/foo.html",
			Ref:       "example.com",
			RefScheme: "h",
		}},

		{"full", url.Values{"p": {"/foo.html"}, "t": {"XX"}, "r": {"https://example.com?p=x"}, "s": {"40,50,1"}}, nil, 200, goatcounter.Hit{
			Path:      "/foo.html",
			Ref:       "example.com",
			RefScheme: "h",
			Width:     new(int16(40)),
		}},

		{"campaign", url.Values{"p": {"/foo.html"}, "q": {"ref=AAA"}}, nil, 200, goatcounter.Hit{
			Path:      "/foo.html",
			Ref:       "AAA",
			RefScheme: "c",
		}},
		{"campaign_override", url.Values{"p": {"/foo.html?ref=AAA"}, "q": {"ref=AAA"}}, nil, 200, goatcounter.Hit{
			Path:      "/foo.html",
			Ref:       "AAA",
			RefScheme: "c",
		}},

		{"width,height,dpr", url.Values{"p": {"/a"}, "s": {"1920,1080,2"}}, nil, 200, goatcounter.Hit{
			Path:  "/a",
			Width: new(int16(1920)),
		}},
		{"width,height", url.Values{"p": {"/a"}, "s": {"1920,1080"}}, nil, 200, goatcounter.Hit{
			Path:  "/a",
			Width: new(int16(1920)),
		}},
		{"width", url.Values{"p": {"/a"}, "s": {"1920"}}, nil, 200, goatcounter.Hit{
			Path:  "/a",
			Width: new(int16(1920)),
		}},

		{"bot", url.Values{"p": {"/a"}, "b": {"150"}}, nil, 200, goatcounter.Hit{
			Path: "/a",
			Bot:  150,
		}},
		{"googlebot", url.Values{"p": {"/a"}, "b": {"150"}}, func(r *http.Request) {
			r.Header.Set("User-Agent", "GoogleBot/1.0")
		}, 200, goatcounter.Hit{
			Path:            "/a",
			Bot:             int(isbot.BotShort),
			UserAgentHeader: "GoogleBot/1.0",
		}},

		{"bot", url.Values{"p": {"/a"}, "b": {"100"}}, nil, 400, goatcounter.Hit{}},

		{"post", url.Values{"p": {"/foo.html"}}, func(r *http.Request) {
			r.Method = "POST"
		}, 200, goatcounter.Hit{
			Path: "/foo.html",
		}},

		{"long path", url.Values{"p": []string{"/" + strings.Repeat("a", 2047)}}, nil, 200, goatcounter.Hit{
			Path: "/" + strings.Repeat("a", 2047),
		}},
		{"too long", url.Values{"p": []string{"/" + strings.Repeat("a", 2048)}}, nil, 400, goatcounter.Hit{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testenv.DB(t)
			ctx = ztime.WithNow(ctx, ztime.FromString("2019-06-18 14:42:00"))

			site := Site(ctx)
			site.Settings.Collect.Set(goatcounter.CollectHits)
			if err := site.Update(ctx); err != nil {
				t.Fatal(err)
			}

			r, rr := newTest(ctx, "GET", "/count?"+tt.query.Encode(), nil)
			if tt.set != nil {
				tt.set(r)
			}
			login(t, r)

			newBackend(ctx).ServeHTTP(rr, r)
			if h := rr.Header().Get("X-Goatcounter"); h != "" {
				t.Logf("X-Goatcounter: %s", h)
			}
			ztest.Code(t, rr, tt.wantCode)

			if tt.wantCode >= 400 {
				return
			}

			_, err := goatcounter.Memstore.Persist(ctx)
			if err != nil {
				t.Fatal(err)
			}

			if tt.hit.UserAgentHeader == "" {
				tt.hit.UserAgentHeader = "GoatCounter test runner/1.0"
			}

			var hits goatcounter.Hits
			err = hits.TestList(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(tt.name, "bot") {
				if len(hits) != 0 {
					t.Fatalf("len(hits) = %d: %#v", len(hits), hits)
				}
				have := zdb.DumpString(ctx, `select * from bots`, zdb.DumpVertical)
				want := ztest.NormalizeIndent(fmt.Sprintf(`
					site        example.com
					path        %s
					bot         %d
					user_agent  %s
					created_at  2019-06-18 14:42:00
				`, tt.hit.Path, tt.hit.Bot, tt.hit.UserAgentHeader))
				if d := ztest.Diff(have, want); d != "" {
					t.Error(d)
				}
				return
			}

			if len(hits) != 1 {
				t.Fatalf("len(hits) = %d: %#v", len(hits), hits)
			}

			h := hits[0]
			err = h.Validate(ctx, false)
			if err != nil {
				t.Errorf("Validate failed after get: %s", err)
			}

			tt.hit.ID = h.ID
			tt.hit.CreatedAt = ztime.Now(ctx)
			tt.hit.Session = goatcounter.TestSeqSession // Should all be the same session.
			h.CreatedAt = h.CreatedAt.In(time.UTC)
			if d := ztest.Diff(string(zjson.MustMarshal(h)), string(zjson.MustMarshal(tt.hit)), ztest.DiffJSON); d != "" {
				t.Error(d)
			}
		})
	}
}

func TestBackendCountSessions(t *testing.T) {
	ctx := testenv.DB(t)
	ctx = ztime.WithNow(ctx, time.Date(2019, 6, 18, 14, 42, 0, 0, time.UTC))

	site := Site(ctx)
	site.Settings.Collect.Set(goatcounter.CollectHits)
	if err := site.Update(ctx); err != nil {
		t.Fatal(err)
	}

	send := func(ctx context.Context, ua string) {
		query := url.Values{"p": {"/" + zcrypto.Secret64()}}

		r, rr := newTest(ctx, "GET", "/count?"+query.Encode(), nil)
		r.Header.Set("User-Agent", ua)
		newBackend(ctx).ServeHTTP(rr, r)
		if h := rr.Header().Get("X-Goatcounter"); h != "" {
			t.Logf("X-Goatcounter: %s", h)
		}
		ztest.Code(t, rr, 200)

		_, err := goatcounter.Memstore.Persist(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}

	checkHits := func(ctx context.Context, n int) goatcounter.Hits {
		var hits goatcounter.Hits
		err := hits.TestList(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if len(hits) != n {
			t.Errorf("len(hits) = %d; wanted %d", len(hits), n)
			for _, h := range hits {
				t.Logf("ID: %d; Session: %d\n", h.ID, h.Session)
			}
			t.Fatal()
		}

		for _, h := range hits {
			err := h.Validate(ctx, false)
			if err != nil {
				t.Errorf("Validate failed after get: %s", err)
			}
		}
		return hits
	}

	// Sessions are keyed on User-Agent + remote address, so the same UA should
	// re-use the same session until it's evicted.
	checkSess := func(hits goatcounter.Hits, wantInt []int) {
		t.Helper()

		first := zint.Uint128{goatcounter.TestSession[0], goatcounter.TestSession[1] + 1}
		want := make([]zint.Uint128, len(wantInt))
		for i := range wantInt {
			want[i] = first
			want[i][1] += uint64(wantInt[i])
		}

		var got []zint.Uint128
		for _, h := range hits {
			got = append(got, h.Session)
			if !h.FirstVisit {
				t.Errorf("FirstVisit is false for %v", h)
			}
		}

		sort.Slice(want, func(i, j int) bool { return want[i][1] < want[j][1] })
		sort.Slice(got, func(i, j int) bool { return got[i][1] < got[j][1] })

		var w, g strings.Builder
		for _, ww := range want {
			w.WriteString(ww.Format(16) + " ")
		}
		for _, gg := range got {
			g.WriteString(gg.Format(16) + " ")
		}
		if w.String() != g.String() {
			t.Errorf("wrong session\nwant: %s\ngot:  %s", w.String(), g.String())
		}
	}

	var (
		ua1 = `Mozilla/5.0 (X11; Linux x86_64; rv:139.0) Gecko/20100101 Firefox/139.0`
		ua2 = `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36`
	)

	send(ctx, ua1)
	send(ctx, ua1)
	send(ctx, ua2)
	send(ctx, ua1)
	checkSess(checkHits(ctx, 4), []int{1, 1, 2, 1})

	// Should still use the same sessions.
	goatcounter.SessionTime = 1 * time.Second
	goatcounter.Memstore.EvictSessions(ctx)
	send(ctx, ua1)
	checkSess(checkHits(ctx, 5), []int{1, 1, 2, 1, 1})

	// Should use new sessions from now on.
	ctx = ztime.WithNow(ctx, time.Date(2019, 6, 18, 14, 42, 2, 0, time.UTC))
	goatcounter.Memstore.EvictSessions(ctx)
	send(ctx, ua1)
	checkSess(checkHits(ctx, 6), []int{1, 1, 2, 1, 1, 3})
}
