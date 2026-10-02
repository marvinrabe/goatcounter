package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/marvinrabe/goatcounter/internal/analytics"
)

// Site calls analytics.MustGetSite; it's just shorter :-)
func Site(ctx context.Context) *analytics.Site { return analytics.MustGetSite(ctx) }

type Globals struct {
	Context         context.Context
	Site            *analytics.Site
	Sites           []analytics.Site
	Path            string
	StaticDomain    string
	TZName          string
	TZOffsetDisplay string
	HideUI          bool
}

// Asset resolves a source asset to its Vite-generated, content-hashed URL.
func (g Globals) Asset(name string) (string, error) {
	paths, err := assetPaths()
	if err != nil {
		return "", err
	}
	file, ok := paths[name]
	if !ok {
		return "", fmt.Errorf("asset %q is missing from Vite manifest", name)
	}
	return "/" + file, nil
}

func newGlobals(r *http.Request) Globals {
	ctx := r.Context()
	cfg := analytics.Config(ctx)
	g := Globals{
		Context: ctx,
		Site:    analytics.GetSite(ctx),
		Sites:   cfg.Sites,
		Path:    r.URL.Path,

		TZName:          cfg.Timezone.Abbr(),
		TZOffsetDisplay: cfg.Timezone.OffsetDisplay(),
		StaticDomain:    r.Host,
		HideUI:          r.URL.Query().Get("hideui") != "",
	}
	return g
}

// ErrPage logs internal errors and renders a safe response for the client.
func ErrPage(w http.ResponseWriter, r *http.Request, reported error) {
	if reported == nil {
		return
	}
	hasStatus := true
	if ww, ok := w.(statusWriter); !ok || ww.Status() == 0 {
		hasStatus = false
	}

	code, userErr := userError(reported)
	if code >= 500 {
		slog.With("module", "http-500").ErrorContext(r.Context(), "HTTP request failed",
			"error", reported, requestAttrs(r))
	}

	ct := strings.ToLower(r.Header.Get("Content-Type"))
	ctresp := strings.ToLower(w.Header().Get("Content-Type"))
	switch {
	case strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ctresp, "application/json"):
		if !hasStatus {
			w.WriteHeader(code)
		}

		j, _ := json.Marshal(map[string]string{"error": userErr.Error()})
		w.Write(j)

	case strings.HasPrefix(ct, "text/plain") || strings.HasPrefix(ctresp, "text/plain"):
		if !hasStatus {
			w.WriteHeader(code)
		}
		fmt.Fprintf(w, "Error %d: %s", code, userErr)

	default:
		if !hasStatus {
			w.WriteHeader(code)
		}

		t := pageTemplates.Load()
		if t == nil || t.Lookup("error.gohtml") == nil {
			fmt.Fprintf(w, "<pre>Error %d: %s</pre>", code, template.HTMLEscapeString(userErr.Error()))
			return
		}

		styleURL, _ := newGlobals(r).Asset("assets/css/backend.css")
		err := t.ExecuteTemplate(w, "error.gohtml", struct {
			Code     int
			Error    error
			Path     string
			StyleURL string
		}{code, userErr, r.URL.Path, styleURL})
		if err != nil {
			slog.ErrorContext(r.Context(), "render error page", "error", err, requestAttrs(r))
		}
	}
}

// Keep request details alongside errors without logging request bodies or query
// parameters, which may contain credentials or other sensitive values.
func requestAttrs(r *http.Request) slog.Attr {
	return slog.Group("http", "method", r.Method, "path", r.URL.Path,
		"host", r.Host, "ua", r.UserAgent())
}
