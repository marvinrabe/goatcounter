package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/httpx"
	"github.com/marvinrabe/goatcounter/internal/validation"
)

// Site calls goatcounter.MustGetSite; it's just shorter :-)
func Site(ctx context.Context) *goatcounter.Site { return goatcounter.MustGetSite(ctx) }

type Globals struct {
	Context         context.Context
	Site            *goatcounter.Site
	Sites           []goatcounter.Site
	Path            string
	Base            string
	Static          string
	StaticDomain    string
	Dev             bool
	TZName          string
	TZOffset        int
	TZOffsetDisplay string
	HideUI          bool
	assetPaths      map[string]string
	assetErr        error
}

// Asset resolves a source asset to its Vite-generated, content-hashed URL.
func (g Globals) Asset(name string) (string, error) {
	paths, err := g.assetPaths, g.assetErr
	if paths == nil && err == nil {
		paths, err = goatcounter.AssetPaths(g.Context)
	}
	if err != nil {
		return "", err
	}
	file, ok := paths[name]
	if !ok {
		return "", fmt.Errorf("asset %q is missing from Vite manifest", name)
	}
	return g.Static + "/" + file, nil
}

func newGlobals(r *http.Request) Globals {
	ctx := r.Context()
	cfg := goatcounter.Config(ctx)
	base := cfg.BasePath
	path := strings.TrimPrefix(r.URL.Path, base)
	if path == "" {
		path = "/"
	}
	g := Globals{
		Context: ctx,
		Site:    goatcounter.GetSite(ctx),
		Sites:   cfg.Sites,
		Path:    path,
		Base:    base,
		Static:  base,
		Dev:     cfg.Dev,

		TZName:          cfg.Timezone.Abbr(),
		TZOffset:        cfg.Timezone.Offset(),
		TZOffsetDisplay: cfg.Timezone.OffsetDisplay(),
		StaticDomain:    r.Host,
		HideUI:          r.URL.Query().Get("hideui") != "",
	}
	g.assetPaths, g.assetErr = goatcounter.AssetPaths(ctx)
	if cfg.DomainStatic != "" {
		g.Static = "//" + cfg.DomainStatic
		g.StaticDomain = cfg.DomainStatic
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

	code, userErr := httpx.UserError(reported)
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

		var (
			j   []byte
			err error
		)

		var validationPtr *validation.Validator
		var validationValue validation.Validator
		if errors.As(userErr, &validationPtr) {
			j, err = json.Marshal(validationPtr)
		} else if errors.As(userErr, &validationValue) {
			j, err = json.Marshal(validationValue)
		} else if jErr, ok := userErr.(json.Marshaler); ok {
			j, err = jErr.MarshalJSON()
		} else {
			j, err = json.Marshal(map[string]string{"error": userErr.Error()})
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "marshal error response", "error", err, requestAttrs(r))
		}
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

		err := t.ExecuteTemplate(w, "error.gohtml", struct {
			Code  int
			Error error
			Base  string
			Path  string
		}{code, userErr, goatcounter.Config(r.Context()).BasePath, r.URL.Path})
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
