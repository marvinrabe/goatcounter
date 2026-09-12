package handlers

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zdb"
	"zgo.at/zstd/zjson"
	"zgo.at/zstd/ztest"
	"zgo.at/zstd/ztime"
)

func TestBackendTpl(t *testing.T) {
	tests := []struct {
		page, want string
	}{
		{"/settings/main", "Data retention in days"},
		{"/settings/purge", "Remove or merge pageviews"},
	}

	for _, tt := range tests {
		t.Run(tt.page, func(t *testing.T) {
			ctx := testenv.DB(t)

			r, rr := newTest(ctx, "GET", tt.page, nil)
			login(t, r)

			newBackend(ctx).ServeHTTP(rr, r)
			ztest.Code(t, rr, 200)

			if !strings.Contains(rr.Body.String(), tt.want) {
				t.Errorf("doesn't contain %q in: %s", tt.want, rr.Body.String())
			}
		})
	}
}

func TestBackendPagesMore(t *testing.T) {
	ctx := testenv.DB(t)
	now := ztime.Now(ctx)

	testenv.StoreHits(ctx, t, false,
		goatcounter.Hit{FirstVisit: true, Path: "/1"},
		goatcounter.Hit{FirstVisit: true, Path: "/2"},
		goatcounter.Hit{FirstVisit: true, Path: "/3"},
		goatcounter.Hit{FirstVisit: true, Path: "/4"},
		goatcounter.Hit{FirstVisit: true, Path: "/5"},
		goatcounter.Hit{FirstVisit: true, Path: "/6"},
		goatcounter.Hit{FirstVisit: true, Path: "/7"},
		goatcounter.Hit{FirstVisit: true, Path: "/8"},
		goatcounter.Hit{FirstVisit: true, Path: "/9"},
		goatcounter.Hit{FirstVisit: true, Path: "/10"},
	)
	url := fmt.Sprintf(
		"/load-widget?widget=1&exclude=1,2,3,4,5&max=10&period-start=%s&period-end=%s",
		now.Format("2006-01-02"), now.Format("2006-01-02"))

	r, rr := newTest(ctx, "GET", url, nil)
	login(t, r)
	newBackend(ctx).ServeHTTP(rr, r)
	ztest.Code(t, rr, 200)

	var body map[string]any
	zjson.MustUnmarshal(rr.Body.Bytes(), &body)

	haveHTML := grep("tr id=", string(body["html"].(string)))
	wantHTML := `
        <tr id="/10" data-id="10" data-count="1"
        <tr id="/9" data-id="9" data-count="1"
        <tr id="/8" data-id="8" data-count="1"
        <tr id="/7" data-id="7" data-count="1"
        <tr id="/6" data-id="6" data-count="1"`

	delete(body, "html")
	haveJSON := string(zjson.MustMarshalIndent(body, "", "\t"))
	wantJSON := `{
		"max": 10,
		"more": false,
		"total_display": 5
	}`

	if d := ztest.Diff(haveHTML, wantHTML, ztest.DiffNormalizeWhitespace); d != "" {
		t.Error(d)
	}
	if d := ztest.Diff(haveJSON, wantJSON, ztest.DiffNormalizeWhitespace); d != "" {
		t.Error(d)
	}
}

func BenchmarkCount(b *testing.B) {
	ctx := testenv.DB(b)

	r, rr := newTest(ctx, "GET", "/count", nil)
	r.URL.RawQuery = url.Values{
		"p": {"/test.html"},
		"t": {"Benchmark test for /count"},
		"r": {"https://example.com/foo"},
	}.Encode()
	r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:72.0) Gecko/20100101 Firefox/72.0")
	r.Header.Set("Referer", "https://example.com/foo")

	handler := newBackend(ctx).ServeHTTP

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler(rr, r)
	}
}

func grep(pat, lines string) string {
	s := strings.Split(lines, "\n")
	r := make([]string, 0, len(s)/2)
	re := regexp.MustCompile(pat)
	for _, l := range s {
		if re.MatchString(l) {
			r = append(r, l)
		}
	}
	return strings.Join(r, "\n")
}

func newBackend(ctx context.Context) chi.Router {
	return NewBackend(zdb.MustGetDB(ctx), true,
		"example.com", "", 10, NewRatelimits())
}
