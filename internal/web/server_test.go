package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func TestServerSiteIndependentRoutes(t *testing.T) {
	users, err := ParseBasicUsers("admin:password")
	if err != nil {
		t.Fatal(err)
	}
	auth := Auth{Mode: AuthBasic, BasicUsers: users}
	router := New(&analytics.Store{}, Ratelimits{}, auth)
	for _, tt := range []struct {
		path, authorization string
		code                int
	}{
		{"/", "", http.StatusUnauthorized},
		{"/load-widget", "", http.StatusUnauthorized},
		{"/missing", "", http.StatusNotFound},
	} {
		r := httptest.NewRequest(http.MethodGet, tt.path+"?site=unknown", nil)
		r.Header.Set("Authorization", tt.authorization)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tt.code {
			t.Errorf("%s: got %d; want %d", r.URL, w.Code, tt.code)
		}
	}

	auth = Auth{Mode: AuthOIDC, OIDC: &OIDCAuth{}}
	router = New(&analytics.Store{}, Ratelimits{}, auth)
	for path, code := range map[string]int{
		"/auth/callback?error=access_denied": http.StatusUnauthorized,
		"/auth/logout":                       http.StatusFound,
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != code {
			t.Errorf("%s: got %d; want %d", path, w.Code, code)
		}
	}
}

func count(t testing.TB, handler http.Handler, ip, path string) {
	t.Helper()
	r, rr := newTest("POST", "/count?"+url.Values{"p": {path}, "site": {"example.com"}}.Encode(), nil)
	r.RemoteAddr = ip
	r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:72.0) Gecko/20100101 Firefox/72.0")
	handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("POST /count: %d %s", rr.Code, rr.Header().Get("X-Goatcounter"))
	}
}

// Pageviews sent to /count show up in the paginated page list.
func TestServerPagesMore(t *testing.T) {
	store := testenv.Store(t)
	now := time.Now().UTC()
	handler := newServer(store)
	for i := range 10 {
		// /1 has one visit, /10 has ten.
		for v := range i + 1 {
			count(t, handler, fmt.Sprintf("10.0.0.%d", v), fmt.Sprintf("/%d", i+1))
		}
	}

	url := fmt.Sprintf("/load-widget?widget=pages&offset=5&total=10&period-start=%s&period-end=%s",
		now.Format("2006-01-02"), now.Format("2006-01-02"))
	r, rr := newTest("GET", url, nil)
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

// The dashboard renders every widget, with the referrers of the showrefs path,
// and a detail loads with its wrapper.
func TestServerDashboard(t *testing.T) {
	store := testenv.Store(t)
	handler := newServer(store)
	count(t, handler, "10.0.0.1", "/a")

	get := func(url string) string {
		t.Helper()
		r, rr := newTest("GET", url, nil)
		login(t, r)
		handler.ServeHTTP(rr, r)
		testenv.Code(t, rr, 200)
		return rr.Body.String()
	}

	page := get("/?showrefs=/a")
	var reload map[string]any
	testenv.MustUnmarshal([]byte(get("/?reload=t&showrefs=/a")), &reload)
	for _, html := range []string{page, reload["widgets"].(string)} {
		for _, want := range []string{`data-widget="totals"`, `data-widget="browsers"`, `class="rows pages"`, `rlink`,
			`class="hchart detail ml-3 border-l-2 border-slate-200 dark:border-neutral-700 pl-4" data-widget="pages" data-key="/a" data-total="1"`} {
			if !strings.Contains(html, want) {
				t.Errorf("missing %q", want)
			}
		}
	}

	var detail map[string]any
	testenv.MustUnmarshal([]byte(get("/load-widget?widget=pages&key=/a&total=1")), &detail)
	if html := detail["html"].(string); !strings.HasPrefix(html, `<div class="hchart detail`) || !strings.Contains(html, "Direct / none") {
		t.Errorf("detail: %s", html)
	}

	r, rr := newTest("GET", "/load-widget?widget=devices&key=Mobile", nil)
	login(t, r)
	handler.ServeHTTP(rr, r)
	testenv.Code(t, rr, 400)
}

func TestCountMethod(t *testing.T) {
	store := testenv.Store(t)
	r, rr := newTest("GET", "/count?p=/x&site=example.com", nil)
	newServer(store).ServeHTTP(rr, r)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("GET /count: %d; want a 4xx error", rr.Code)
	}
}

func BenchmarkCount(b *testing.B) {
	store := testenv.Store(b)
	handler := newServer(store)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count(b, handler, "10.0.0.1", "/test.html")
	}
}

func newServer(store *analytics.Store) chi.Router {
	return New(store, Ratelimits{}, Auth{Mode: AuthPublic})
}
