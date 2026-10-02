package web

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
	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func TestBackendSiteIndependentRoutes(t *testing.T) {
	ctx := analytics.NewConfig(context.Background())
	users, err := ParseBasicUsers("admin:password")
	if err != nil {
		t.Fatal(err)
	}
	auth := Auth{Mode: AuthBasic, BasicUsers: users}
	router := New(nil, 10, Ratelimits{}, auth)
	for _, tt := range []struct {
		path, authorization string
		code                int
	}{
		{"/", "", http.StatusUnauthorized},
		{"/load-widget", "", http.StatusUnauthorized},
		{"/missing", "", http.StatusNotFound},
	} {
		r := httptest.NewRequest(http.MethodGet, tt.path+"?site=unknown", nil).WithContext(ctx)
		r.Header.Set("Authorization", tt.authorization)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tt.code {
			t.Errorf("%s: got %d; want %d", r.URL, w.Code, tt.code)
		}
	}

	auth = Auth{Mode: AuthOIDC, OIDC: &OIDCAuth{}}
	router = New(nil, 10, Ratelimits{}, auth)
	for path, code := range map[string]int{
		"/auth/callback?error=access_denied": http.StatusUnauthorized,
		"/auth/logout":                       http.StatusFound,
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
		if w.Code != code {
			t.Errorf("%s: got %d; want %d", path, w.Code, code)
		}
	}
}

func count(t testing.TB, ctx context.Context, handler http.Handler, ip, path string) {
	t.Helper()
	r, rr := newTest(ctx, "POST", "/count?"+url.Values{"p": {path}, "s": {"1440"}, "site": {"example.com"}}.Encode(), nil)
	r.RemoteAddr = ip
	r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:72.0) Gecko/20100101 Firefox/72.0")
	handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("POST /count: %d %s", rr.Code, rr.Header().Get("X-Goatcounter"))
	}
}

// Pageviews sent to /count show up in the paginated page list.
func TestBackendPagesMore(t *testing.T) {
	ctx := testenv.DB(t)
	now := datetime.Now(ctx)
	handler := newBackend(ctx)
	for i := range 10 {
		// /1 has one visit, /10 has ten.
		for v := range i + 1 {
			count(t, ctx, handler, fmt.Sprintf("10.0.0.%d", v), fmt.Sprintf("/%d", i+1))
		}
	}

	url := fmt.Sprintf("/load-widget?widget=pages&offset=5&total=10&period-start=%s&period-end=%s",
		now.Format("2006-01-02"), now.Format("2006-01-02"))
	r, rr := newTest(ctx, "GET", url, nil)
	login(t, r)
	handler.ServeHTTP(rr, r)
	testenv.Code(t, rr, 200)

	var body map[string]any
	testenv.MustUnmarshal(rr.Body.Bytes(), &body)
	haveHTML := strings.Join(regexp.MustCompile(`data-id="[^"]+"`).FindAllString(body["html"].(string), -1), " ")
	wantHTML := `data-id="/5" data-id="/4" data-id="/3" data-id="/2" data-id="/1"`
	if d := testenv.Diff(haveHTML, wantHTML); d != "" {
		t.Error(d)
	}
	if body["more"] != false {
		t.Errorf("more=%v", body["more"])
	}
}

// Screen widths are stored; an empty width is unknown, an invalid one an error.
func TestCountWidth(t *testing.T) {
	ctx := testenv.DB(t)
	handler := newBackend(ctx)
	for s, code := range map[string]int{"1440": 204, "": 204, "-5": 400, "abc": 400, "999999": 400} {
		r, rr := newTest(ctx, "POST", "/count?"+url.Values{"p": {"/w" + s}, "s": {s}, "site": {"example.com"}}.Encode(), nil)
		r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:72.0) Gecko/20100101 Firefox/72.0")
		handler.ServeHTTP(rr, r)
		if rr.Code != code {
			t.Errorf("s=%q: %d %s; want %d", s, rr.Code, rr.Header().Get("X-Goatcounter"), code)
		}
	}
	var widths []int
	if err := database.Select(ctx, &widths, `select width from events order by path`); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(widths) != "[0 1440]" {
		t.Errorf("stored widths %v; want [0 1440]", widths)
	}
}

func TestCountMethod(t *testing.T) {
	ctx := testenv.DB(t)
	r, rr := newTest(ctx, "GET", "/count?p=/x&site=example.com", nil)
	newBackend(ctx).ServeHTTP(rr, r)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("GET /count: %d; want a 4xx error", rr.Code)
	}
}

func BenchmarkCount(b *testing.B) {
	ctx := testenv.DB(b)
	handler := newBackend(ctx)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count(b, ctx, handler, "10.0.0.1", "/test.html")
	}
}

func newBackend(ctx context.Context) chi.Router {
	return New(database.MustGetDB(ctx), 10, Ratelimits{}, Auth{Mode: AuthPublic})
}
