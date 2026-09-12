package handlers

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/i18n"
	"github.com/marvinrabe/goatcounter/internal/log"
	_ "github.com/marvinrabe/goatcounter/internal/tpl" // Registers template functions.
	"zgo.at/json"
	"zgo.at/zhttp"
	"zgo.at/ztpl"
)

// Site calls goatcounter.MustGetSite; it's just shorter :-)
func Site(ctx context.Context) *goatcounter.Site { return goatcounter.MustGetSite(ctx) }

var T = i18n.T

type Globals struct {
	Context         context.Context
	Site            *goatcounter.Site
	Sites           []goatcounter.Site
	Path            string
	Base            string
	Flash           *zhttp.FlashMessage
	Static          string
	StaticDomain    string
	Domain          string
	Version         string
	Dev             bool
	Port            string
	TZName          string
	TZOffset        int
	TZOffsetDisplay string
	JSTranslations  map[string]string
	HideUI          bool
	assetPaths      map[string]string
	assetErr        error
}

func (g Globals) T(msg string, data ...any) template.HTML {
	return template.HTML(i18n.T(g.Context, msg, data...))
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

func newGlobals(w http.ResponseWriter, r *http.Request) Globals {
	ctx := r.Context()
	base := goatcounter.Config(ctx).BasePath
	path := strings.TrimPrefix(r.URL.Path, base)
	if path == "" {
		path = "/"
	}
	g := Globals{
		Context: ctx,
		Site:    goatcounter.GetSite(ctx),
		Sites:   goatcounter.Config(ctx).Sites,
		Path:    path,
		Base:    base,
		Flash:   zhttp.ReadFlash(w, r),
		Static:  goatcounter.Config(ctx).URLStatic,
		Domain:  goatcounter.Config(ctx).Domain,
		Version: goatcounter.Version,
		Dev:     goatcounter.Config(ctx).Dev,
		Port:    goatcounter.Config(ctx).Port,

		TZName:          goatcounter.Config(ctx).Timezone.Abbr(),
		TZOffset:        goatcounter.Config(ctx).Timezone.Offset(),
		TZOffsetDisplay: goatcounter.Config(ctx).Timezone.OffsetDisplay(),
		HideUI:          r.URL.Query().Get("hideui") != "",
		JSTranslations: map[string]string{
			"error/date-mismatch":       T(ctx, "error/date-mismatch|end date is before start date"),
			"error/load-url":            T(ctx, "error/load-url|Could not load %(url): %(error)", i18n.P{"url": "%(url)", "error": "%(error)"}),
			"notify/saved":              T(ctx, "notify/saved|Saved!"),
			"datepicker/keyboard":       T(ctx, "datepicker/keyboard|Use the arrow keys to pick a date"),
			"datepicker/month-prev":     T(ctx, "datepicker/month-prev|Previous month"),
			"datepicker/month-next":     T(ctx, "datepicker/month-next|Next month"),
			"nav-dash/filter-more-help": T(ctx, "nav-dash/filter-more-help|More help"),
			"nav-fash/filter-less-help": T(ctx, "nav-fash/filter-less-help|Less help"),
		},
	}
	g.assetPaths, g.assetErr = goatcounter.AssetPaths(ctx)
	if goatcounter.Config(r.Context()).DomainStatic == "" {
		s := goatcounter.GetSite(r.Context())
		if s != nil {
			g.StaticDomain = s.Domain(r.Context())
		} else {
			g.StaticDomain = "/"
		}
	} else {
		g.StaticDomain = goatcounter.Config(r.Context()).DomainStatic
	}

	return g
}

// Identical to the default errpage, but replaces slog calls with our log calls.
func ErrPage(w http.ResponseWriter, r *http.Request, reported error) {
	if reported == nil {
		return
	}
	hasStatus := true
	if ww, ok := w.(statusWriter); !ok || ww.Status() == 0 {
		hasStatus = false
	}

	code, userErr := zhttp.UserError(reported)
	if code >= 500 {
		l := log.Module("http-500")
		l = l.With("code", zhttp.UserErrorCode(reported))

		sErr := new(interface{ StackTrace() string })
		if errors.As(reported, sErr) {
			reported = errors.Unwrap(reported)
			l = l.With("stacktrace", "\n"+(*sErr).StackTrace())
		}
		l.Error(r.Context(), reported, log.AttrHTTP(r))
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

		if jErr, ok := userErr.(json.Marshaler); ok {
			j, err = jErr.MarshalJSON()
		} else if jErr, ok := userErr.(interface{ ErrorJSON() ([]byte, error) }); ok {
			j, err = jErr.ErrorJSON()
		} else {
			j, err = json.Marshal(map[string]string{"error": userErr.Error()})
		}
		if err != nil {
			log.Error(r.Context(), err, log.AttrHTTP(r))
		}
		w.Write(j)

	case strings.HasPrefix(ct, "text/plain") || strings.HasPrefix(ctresp, "text/plain"):
		if !hasStatus {
			w.WriteHeader(code)
		}
		fmt.Fprintf(w, "Error %d: %s", code, userErr)

	case (!hasStatus && r.Referer() != "" &&
		(ct == "application/x-www-form-urlencoded" || ctresp == "application/x-www-form-urlencoded")) ||
		(strings.HasPrefix(ct, "multipart/") || strings.HasPrefix(ctresp, "multipart/")):
		zhttp.FlashError(w, r, userErr.Error())
		zhttp.SeeOther(w, r.Referer())

	default:
		if !hasStatus {
			w.WriteHeader(code)
		}

		if !ztpl.HasTemplate("error.gohtml") {
			fmt.Fprintf(w, "<pre>Error %d: %s</pre>", code, userErr)
			return
		}

		err := ztpl.Execute(w, "error.gohtml", struct {
			Code  int
			Error error
			Base  string
			Path  string
		}{code, userErr, zhttp.BasePath, r.URL.Path})
		if err != nil {
			log.Error(r.Context(), err, log.AttrHTTP(r))
		}
	}
}
