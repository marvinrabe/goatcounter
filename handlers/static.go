package handlers

import (
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter"
)

// NewStatic serves public assets from disk in development and embeds in production.
func NewStatic(r chi.Router, dev bool, basePath string) chi.Router {
	var files fs.FS
	var err error
	if dev {
		files = os.DirFS("public")
	} else {
		files, err = fs.Sub(goatcounter.Static, "public")
		if err != nil {
			panic(err)
		}
	}
	server := http.FileServerFS(files)
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		r = r.Clone(r.Context())
		r.URL.Path = strings.TrimPrefix(r.URL.Path, basePath)
		path := strings.TrimPrefix(r.URL.Path, "/")
		info, err := fs.Stat(files, path)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		cache := "no-cache"
		switch {
		case dev:
			cache = "no-store,no-cache"
		case r.URL.Path == "/count.js":
			cache = "public, max-age=604800"
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			cache = "public, max-age=31536000"
		}
		w.Header().Set("Cache-Control", cache)
		if r.URL.Path == "/count.js" {
			w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
		}
		server.ServeHTTP(w, r)
	})
	return r
}
