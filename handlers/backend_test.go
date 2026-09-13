package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"github.com/marvinrabe/goatcounter/internal/testutil"
)

func TestBackendSiteIndependentRoutes(t *testing.T) {
	for _, base := range []string{"", "/stats"} {
		ctx := goatcounter.NewConfig(context.Background())
		goatcounter.Config(ctx).BasePath = base
		users, err := ParseBasicUsers("admin:password")
		if err != nil {
			t.Fatal(err)
		}
		auth := Auth{Mode: AuthBasic, BasicUsers: users}
		router := NewBackend(nil, false, "", base, 10, Ratelimits{}, "secret", auth)
		for _, tt := range []struct {
			path, authorization string
			code                int
		}{
			{"/", "", http.StatusUnauthorized},
			{"/load-widget", "", http.StatusUnauthorized},
			{"/api", "", http.StatusUnauthorized},
			{"/api", "Bearer secret", http.StatusOK},
			{"/missing", "", http.StatusNotFound},
		} {
			r := httptest.NewRequest(http.MethodGet, base+tt.path+"?site=unknown", nil).WithContext(ctx)
			r.Header.Set("Authorization", tt.authorization)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tt.code {
				t.Errorf("%s: got %d; want %d", r.URL, w.Code, tt.code)
			}
		}

		auth = Auth{Mode: AuthOIDC, OIDC: &OIDCAuth{basePath: base}}
		router = NewBackend(nil, false, "", base, 10, Ratelimits{}, "", auth)
		for path, code := range map[string]int{
			"/auth/callback?error=access_denied": http.StatusUnauthorized,
			"/auth/logout":                       http.StatusFound,
		} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, base+path, nil).WithContext(ctx))
			if w.Code != code {
				t.Errorf("%s%s: got %d; want %d", base, path, w.Code, code)
			}
		}
	}
}

func TestBackendPagesMore(t *testing.T) {
	ctx := testenv.DB(t)
	now := datetime.Now(ctx)

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
		"/load-widget?widget=1&exclude=1,2,3,4,5&max=10&total=10&period-start=%s&period-end=%s",
		now.Format("2006-01-02"), now.Format("2006-01-02"))

	r, rr := newTest(ctx, "GET", url, nil)
	login(t, r)
	newBackend(ctx).ServeHTTP(rr, r)
	testutil.Code(t, rr, 200)

	var body map[string]any
	testutil.MustUnmarshal(rr.Body.Bytes(), &body)

	haveHTML := grep(`<div class="hchart-row`, string(body["html"].(string)))
	wantHTML := `
		<div class="hchart-row" id="/10" data-id="10" data-key="10" data-count="1" data-detail-total="1">
		<div class="hchart-row" id="/9" data-id="9" data-key="9" data-count="1" data-detail-total="1">
		<div class="hchart-row" id="/8" data-id="8" data-key="8" data-count="1" data-detail-total="1">
		<div class="hchart-row" id="/7" data-id="7" data-key="7" data-count="1" data-detail-total="1">
		<div class="hchart-row" id="/6" data-id="6" data-key="6" data-count="1" data-detail-total="1">`

	delete(body, "html")
	haveJSON := string(testutil.MustMarshalIndent(body, "", "\t"))
	wantJSON := `{
		"max": 10,
		"more": false,
		"total_display": 5
	}`

	if d := testutil.Diff(haveHTML, wantHTML, testutil.DiffNormalizeWhitespace); d != "" {
		t.Error(d)
	}
	if d := testutil.Diff(haveJSON, wantJSON, testutil.DiffNormalizeWhitespace); d != "" {
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
	return NewBackend(database.MustGetDB(ctx), true,
		"example.com", "", 10, NewRatelimits(), "", Auth{Mode: AuthPublic})
}
