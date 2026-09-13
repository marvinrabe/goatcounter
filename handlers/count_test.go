package handlers

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter"
	botcheck "github.com/marvinrabe/goatcounter/internal/bot"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"github.com/marvinrabe/goatcounter/internal/testutil"
)

func TestBackendCountLanguage(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"collected", "nl-BE,nl;q=0.9", "nld"},
		{"no header", "", ""},
		{"unknown language", "xx", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testenv.DB(t)
			ctx = datetime.WithNow(ctx, datetime.FromString("2019-06-18 14:42:00"))

			r, rr := newTest(ctx, "GET", "/count?p=/foo.html", nil)
			if tt.header != "" {
				r.Header.Set("Accept-Language", tt.header)
			}
			login(t, r)
			newBackend(ctx).ServeHTTP(rr, r)
			testutil.Code(t, rr, 200)

			if _, err := goatcounter.PersistHits(ctx, nil); err != nil {
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

		{"campaign", url.Values{"p": {"/foo.html?ref=AAA"}}, nil, 200, goatcounter.Hit{
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
			Bot:             int(botcheck.BotShort),
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
			ctx = datetime.WithNow(ctx, datetime.FromString("2019-06-18 14:42:00"))

			r, rr := newTest(ctx, "GET", "/count?"+tt.query.Encode(), nil)
			if tt.set != nil {
				tt.set(r)
			}
			login(t, r)

			newBackend(ctx).ServeHTTP(rr, r)
			if h := rr.Header().Get("X-Goatcounter"); h != "" {
				t.Logf("X-Goatcounter: %s", h)
			}
			testutil.Code(t, rr, tt.wantCode)

			if tt.wantCode >= 400 {
				return
			}

			_, err := goatcounter.PersistHits(ctx, nil)
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
				have := database.DumpString(ctx, `select * from bots`, true)
				want := testutil.NormalizeIndent(fmt.Sprintf(`
					site        example.com
					path        %s
					bot         %d
					user_agent  %s
					created_at  2019-06-18 14:42:00
				`, tt.hit.Path, tt.hit.Bot, tt.hit.UserAgentHeader))
				if d := testutil.Diff(have, want); d != "" {
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
			tt.hit.CreatedAt = datetime.Now(ctx)
			tt.hit.Session = h.Session // Should all be the same session.
			h.CreatedAt = h.CreatedAt.In(time.UTC)
			if d := testutil.Diff(string(testutil.MustMarshal(h)), string(testutil.MustMarshal(tt.hit)), testutil.DiffJSON); d != "" {
				t.Error(d)
			}
		})
	}
}

func TestBackendCountSessions(t *testing.T) {
	ctx := testenv.DB(t)
	ctx = datetime.WithNow(ctx, time.Date(2019, 6, 18, 14, 42, 0, 0, time.UTC))

	send := func(ctx context.Context, ua string) {
		query := url.Values{"p": {"/" + rand.Text()}}

		r, rr := newTest(ctx, "GET", "/count?"+query.Encode(), nil)
		r.Header.Set("User-Agent", ua)
		newBackend(ctx).ServeHTTP(rr, r)
		if h := rr.Header().Get("X-Goatcounter"); h != "" {
			t.Logf("X-Goatcounter: %s", h)
		}
		testutil.Code(t, rr, 200)

		_, err := goatcounter.PersistHits(ctx, nil)
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

		var got []goatcounter.SessionID
		for _, h := range hits {
			got = append(got, h.Session)
			if !h.FirstVisit {
				t.Errorf("FirstVisit is false for %v", h)
			}
		}

		// Assert session equivalence and multiplicity, without depending on UUIDs.
		wantCounts := map[int]int{}
		for _, id := range wantInt {
			wantCounts[id]++
		}
		gotCounts := map[goatcounter.SessionID]int{}
		for _, id := range got {
			if id == (goatcounter.SessionID{}) {
				t.Fatal("zero session")
			}
			gotCounts[id]++
		}
		var want, have []int
		for _, n := range wantCounts {
			want = append(want, n)
		}
		for _, n := range gotCounts {
			have = append(have, n)
		}
		sort.Ints(want)
		sort.Ints(have)
		if fmt.Sprint(want) != fmt.Sprint(have) {
			t.Errorf("session visit counts: %v; want %v", have, want)
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
	previousLifetime := goatcounter.SessionTime
	t.Cleanup(func() { goatcounter.SessionTime = previousLifetime })
	goatcounter.SessionTime = 1 * time.Second
	goatcounter.EvictSessions(ctx)
	send(ctx, ua1)
	checkSess(checkHits(ctx, 5), []int{1, 1, 2, 1, 1})

	// Should use new sessions from now on.
	ctx = datetime.WithNow(ctx, time.Date(2019, 6, 18, 14, 42, 2, 0, time.UTC))
	goatcounter.EvictSessions(ctx)
	send(ctx, ua1)
	checkSess(checkHits(ctx, 6), []int{1, 1, 2, 1, 1, 3})
}

func TestCollectorDoesNotAcknowledgeFailedEnqueue(t *testing.T) {
	ctx := testenv.DB(t)
	if err := database.Exec(ctx, `create trigger reject_enqueue before insert on hit_queue
        begin select raise(abort, 'queue unavailable'); end`); err != nil {
		t.Fatal(err)
	}
	r, w := newTest(ctx, "GET", "/count?p=/must-not-ack", nil)
	newBackend(ctx).ServeHTTP(w, r)
	if w.Code < 500 {
		t.Fatalf("failed durable enqueue returned HTTP %d", w.Code)
	}
	var n int
	if err := database.Get(ctx, &n, `select count(*) from hit_queue`); err != nil || n != 0 {
		t.Fatalf("queued=%d, error=%v", n, err)
	}
}
