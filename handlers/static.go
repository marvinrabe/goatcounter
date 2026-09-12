package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter"
	"zgo.at/zhttp"
	"zgo.at/zstd/zfs"
)

// NewStatic serves the generated public/ directory, embedded in production
// and read from disk in development. It does not require a database or site.
func NewStatic(r chi.Router, dev bool, basePath string) chi.Router {
	cache := map[string]int{"": zhttp.CacheNoStore}
	if !dev {
		cache = map[string]int{
			"/count.js": 86400 * 7,
			"/assets/*": 86400 * 365,
			"":          zhttp.CacheNoCache,
		}
	}
	fsys, err := zfs.EmbedOrDir(goatcounter.Static, "public", dev)
	if err != nil {
		panic(err)
	}

	s := zhttp.NewStatic("*", fsys, cache)
	s.Header("/count.js", map[string]string{
		"Cross-Origin-Resource-Policy": "cross-origin",
	})
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		// Preserve the original URL for middleware such as request logging.
		r = r.Clone(r.Context())
		r.URL.Path = strings.TrimPrefix(r.URL.Path, basePath)
		s.ServeHTTP(w, r)
	})
	return r
}
