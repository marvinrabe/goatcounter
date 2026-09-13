package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zdb"
	"zgo.at/zstd/ztest"
	"zgo.at/zstd/ztime"
)

func apiBackend(ctx context.Context, token string) chi.Router {
	return NewBackend(zdb.MustGetDB(ctx), true, "example.com", "", 10, NewRatelimits(), token, Auth{Mode: AuthPublic})
}

func callAPI(t *testing.T, ctx context.Context, token, action string, args any) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{"action": action, "arguments": args})
	if err != nil {
		t.Fatal(err)
	}
	r, rr := newTest(ctx, http.MethodPost, "/api", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	apiBackend(ctx, token).ServeHTTP(rr, r)
	ztest.Code(t, rr, http.StatusOK)
	var result map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAPIDisabledAndBearerAuth(t *testing.T) {
	ctx := testenv.DB(t)
	r, rr := newTest(ctx, http.MethodGet, "/api", nil)
	apiBackend(ctx, "").ServeHTTP(rr, r)
	ztest.Code(t, rr, http.StatusNotFound)

	r, rr = newTest(ctx, http.MethodGet, "/api", nil)
	apiBackend(ctx, "secret").ServeHTTP(rr, r)
	ztest.Code(t, rr, http.StatusUnauthorized)

	// Public dashboard access does not authorize the API; neither do valid
	// dashboard Basic credentials.
	r, rr = newTest(ctx, http.MethodGet, "/api", nil)
	r.SetBasicAuth("dashboard", "password")
	users, err := ParseBasicUsers("dashboard:password")
	if err != nil {
		t.Fatal(err)
	}
	basic := Auth{Mode: AuthBasic, BasicUsers: users}
	NewBackend(zdb.MustGetDB(ctx), true, "example.com", "", 10, NewRatelimits(), "secret", basic).ServeHTTP(rr, r)
	ztest.Code(t, rr, http.StatusUnauthorized)

	r, rr = newTest(ctx, http.MethodGet, "/settings/purge", nil)
	login(t, r)
	apiBackend(ctx, "secret").ServeHTTP(rr, r)
	ztest.Code(t, rr, http.StatusNotFound)
}

func TestAPISiteContextDoesNotNeedDatabase(t *testing.T) {
	ctx := testenv.Context(nil)
	ctx, site, err := apiSiteContext(ctx, "example.com")
	if err != nil || site.Key != "example.com" || Site(ctx).Key != site.Key {
		t.Fatalf("configured site: %#v, %v", site, err)
	}
}

func TestAPIPageviews(t *testing.T) {
	ctx := testenv.DB(t)
	now := ztime.Now(ctx).Add(-time.Hour)
	testenv.StoreHits(ctx, t, false,
		goatcounter.Hit{FirstVisit: true, Path: "/asd", CreatedAt: now},
		goatcounter.Hit{FirstVisit: true, Path: "/asd", CreatedAt: now},
		goatcounter.Hit{FirstVisit: true, Path: "/zxc", CreatedAt: now})

	got := callAPI(t, ctx, "secret", "pageviews", map[string]any{"search": "/asd"})
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"path":"/asd"`) || !strings.Contains(string(b), `"count":2`) {
		t.Fatalf("unexpected result: %s", b)
	}
}

func TestAPIMerge(t *testing.T) {
	ctx := testenv.DB(t)
	uaLinux := `Mozilla/5.0 (X11; Linux x86_64; rv:139.0) Gecko/20100101 Firefox/139.0`
	uaMac := `Mozilla/5.0 (Macintosh; Intel Mac OS X 10.14; rv:139.0) Gecko/20100101 Firefox/139.0`
	now := ztime.FromString("2025-06-13 12:13:40")
	testenv.StoreHits(ctx, t, false,
		goatcounter.Hit{FirstVisit: true, CreatedAt: now, Path: "/one", UserAgentHeader: uaLinux},
		goatcounter.Hit{FirstVisit: true, CreatedAt: now, Path: "/two", UserAgentHeader: uaMac})

	callAPI(t, ctx, "secret", "merge_pageviews", map[string]any{"target_path_id": 1, "path_ids": []int{2}})
	var paths goatcounter.Paths
	if _, err := paths.List(ctx, 0, 100); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0].Path != "/one" {
		t.Fatalf("unexpected paths after merge: %#v", paths)
	}
	var total int
	if err := zdb.Get(ctx, &total, `select total from hit_counts where path_id=1`); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("total after merge is %d; want 2", total)
	}
}

func TestAPIMCP(t *testing.T) {
	ctx := testenv.DB(t)
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	r, rr := newTest(ctx, http.MethodPost, "/api", body)
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Content-Type", "application/json")
	apiBackend(ctx, "secret").ServeHTTP(rr, r)
	ztest.Code(t, rr, http.StatusOK)
	if !strings.Contains(rr.Body.String(), `"name":"dashboard"`) || !strings.Contains(rr.Body.String(), `"name":"import_raw"`) {
		t.Fatalf("unexpected MCP tools response: %s", rr.Body.String())
	}
}

func TestAPIImportAndDashboard(t *testing.T) {
	ctx := testenv.DB(t)
	created := ztime.Now(ctx).UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
	callAPI(t, ctx, "secret", "import_raw", map[string]any{
		"replace": true,
		"hits": []map[string]any{
			{"site": "example.com", "path": "/imported", "created_at": created,
				"session": "00112233445566778899aabbccddeeff", "first_visit": true,
				"browser": "Firefox", "browser_version": "143", "system": "Linux", "width": 1920},
			{"site": "example.com", "path": "/second", "created_at": created,
				"session": "00112233445566778899aabbccddeeff", "first_visit": true},
		},
	})

	got := callAPI(t, ctx, "secret", "dashboard", map[string]any{"period": "day", "limit": 10})
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"pageviews":2`) || !strings.Contains(string(b), `"path":"/imported"`) {
		t.Fatalf("unexpected dashboard after import: %s", b)
	}
}

func TestAPIImportDistinctCampaigns(t *testing.T) {
	ctx := testenv.DB(t)
	if err := zdb.Exec(ctx, `insert into campaigns (name) values ('Alpha'), ('Beta')`); err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	callAPI(t, ctx, "secret", "import_raw", map[string]any{
		"hits": []map[string]any{
			{"site": "example.com", "path": "/alpha", "created_at": created, "campaign": "Alpha", "first_visit": true},
			{"site": "example.com", "path": "/beta", "created_at": created, "campaign": "Beta", "first_visit": true},
		},
	})
	var names []string
	if err := zdb.Select(ctx, &names, `select campaigns.name from hits join campaigns on campaigns.campaign_id = hits.campaign order by campaigns.name`); err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "Alpha,Beta" {
		t.Fatalf("imported campaigns: %v", names)
	}
}
