package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter/internal/log"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zhttp"
	"zgo.at/zstd/zgo"
	"zgo.at/zstd/zjson"
	"zgo.at/zstd/zruntime"
	"zgo.at/zstd/ztest"
	"zgo.at/ztpl"
)

type handlerTest struct {
	name         string
	setup        func(context.Context, *testing.T)
	router       func(context.Context) chi.Router
	path         string
	method       string
	auth         bool
	body         any
	wantCode     int
	wantFormCode int
	wantBody     string
	wantFormBody string
}

func init() {

	files, _ := fs.Sub(os.DirFS(zgo.ModuleRoot()), "tpl")
	err := ztpl.Init(files)
	if err != nil {
		panic(err)
	}

	ztest.DefaultHost = "test.example.com"
	if zruntime.TestVerbose() {
		log.SetDebug([]string{"all"})
	} else {
		slog.SetDefault(slog.New(slog.DiscardHandler)) // Don't care about logs; don't spam.
	}
}

func TestMain(m *testing.M) {
	os.Exit(ztpl.TestTemplateExecution(m,
		// Don't need tests.
		"", "error.gohtml",

		// Not executed by any test yet.
		"_dashboard_pages_refs.gohtml",
		"_dashboard_pages_text.gohtml",
		"_dashboard_pages_text_rows.gohtml",
		"_dashboard_configure_widget.gohtml",
		"_user_dashboard_widget.gohtml",
	))
}

func runTest(
	t *testing.T,
	tt handlerTest,
	fun func(*testing.T, *httptest.ResponseRecorder, *http.Request),
) {
	t.Helper()
	if tt.method == "" {
		tt.method = "GET"
	}
	if tt.path == "" {
		tt.path = "/"
	}

	t.Run(tt.name, func(t *testing.T) {
		sn := "json"
		if tt.method == "GET" {
			sn = "html"
		}

		if tt.wantCode > 0 {
			t.Run(sn, func(t *testing.T) {
				ctx := testenv.DB(t)

				r, rr := newTest(ctx, tt.method, tt.path, bytes.NewReader(zjson.MustMarshal(tt.body)))
				if tt.setup != nil {
					tt.setup(ctx, t)
				}
				if tt.auth {
					login(t, r)
				}

				tt.router(ctx).ServeHTTP(rr, r)
				ztest.Code(t, rr, tt.wantCode)
				if !strings.Contains(rr.Body.String(), tt.wantBody) {
					t.Errorf("wrong body\nwant: %s\ngot:  %s", tt.wantBody, rr.Body.String())
				}

				if fun != nil {
					// Don't use request context as it'll get cancelled.
					fun(t, rr, r.WithContext(ctx))
				}
			})
		}

		if tt.method == "GET" {
			return
		}

		t.Run("form", func(t *testing.T) {
			ctx := testenv.DB(t)

			form := formBody(tt.body)
			r, rr := newTest(ctx, tt.method, tt.path, strings.NewReader(form))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Content-Length", fmt.Sprintf("%d", len(form)))
			if tt.setup != nil {
				tt.setup(ctx, t)
			}
			if tt.auth {
				login(t, r)
			}

			tt.router(ctx).ServeHTTP(rr, r)
			if f := zhttp.ReadFlash(rr, r); f != nil {
				t.Logf("flash message (%s): %s", f.Level, f.Message)
			}
			ztest.Code(t, rr, tt.wantFormCode)
			if !strings.Contains(rr.Body.String(), tt.wantFormBody) {
				t.Errorf("wrong body\nwant: %q\ngot:  %q", tt.wantFormBody, rr.Body.String())
			}

			if fun != nil {
				// Don't use request context as it'll get cancelled.
				fun(t, rr, r.WithContext(ctx))
			}
		})
	})
}

func login(t *testing.T, r *http.Request) {
	t.Helper()
	r.SetBasicAuth("test@example.com", "coconuts")
}
func newTest(ctx context.Context, method, path string, body io.Reader) (*http.Request, *httptest.ResponseRecorder) {
	site := Site(ctx)
	r, rr := ztest.NewRequest(method, path, body).WithContext(ctx), httptest.NewRecorder()
	r.Header.Set("User-Agent", "GoatCounter test runner/1.0")
	r.Host = site.Domain(ctx)
	return r, rr
}

// Convert anything to an "application/x-www-form-urlencoded" form.
//
// Use github.com/teamwork/test.Multipart for a multipart form.
//
// Note: this is primitive, but enough for now.
func formBody(i any) string {
	var m map[string]string
	zjson.MustUnmarshal(zjson.MustMarshal(i), &m)

	f := make(url.Values)
	for k, v := range m {
		f[k] = []string{v}
	}

	return f.Encode()
}
