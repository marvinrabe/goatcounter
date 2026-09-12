package handlers

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/bgrun"
	"github.com/marvinrabe/goatcounter/internal/geo"
	"github.com/marvinrabe/goatcounter/internal/log"
	"github.com/monoculum/formam/v3"
	"zgo.at/errors"
	"zgo.at/guru"
	"zgo.at/zhttp"
	"zgo.at/zstd/zint"
	"zgo.at/zvalidate"
)

type settings struct{}

func (h settings) mount(r chi.Router, ratelimits Ratelimits) {
	{ // Site settings.
		set := r.With(loggedIn)

		set.Get("/settings", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			zhttp.SeeOther(w, "/settings/main")
		}))
		set.Get("/settings/main", zhttp.Wrap(func(w http.ResponseWriter, r *http.Request) error {
			return h.main(nil)(w, r)
		}))
		set.Post("/settings/main", zhttp.Wrap(h.mainSave))
		set.Get("/settings/purge", zhttp.Wrap(h.purge))
		set.Post("/settings/purge", zhttp.Wrap(h.purgeDo))
		set.Post("/settings/merge", zhttp.Wrap(h.merge))
	}
}

func (h settings) main(verr *zvalidate.Validator) zhttp.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		cities := false
		geodb := geo.Get(r.Context())
		if geodb == nil {
			log.Error(r.Context(), "geodb is nil")
		} else {
			cities = strings.Contains(strings.ToLower(geodb.Metadata().DatabaseType), "city")
		}

		return zhttp.Template(w, "settings_main.gohtml", struct {
			Globals
			Validate *zvalidate.Validator
			Cities   bool
		}{newGlobals(w, r), verr, cities})
	}
}

func (h settings) mainSave(w http.ResponseWriter, r *http.Request) error {
	v := goatcounter.NewValidate(r.Context())

	site := Site(r.Context())
	args := struct {
		LinkDomain string                   `json:"link_domain"`
		Settings   goatcounter.SiteSettings `json:"settings"`
	}{Settings: site.Settings}

	// Don't use zhttp.Decode to decode forms: without IgnoreUnknownKeys
	// formam stops decoding at the first unknown key and zhttp.Decode
	// silently swallows that error, so an old form containing a
	// since-removed setting would silently drop all other settings as
	// well. Decode with IgnoreUnknownKeys instead, mirroring the /count
	// handler.
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		_, err = zhttp.Decode(r, &args)
	} else {
		err = r.ParseForm()
		if err == nil {
			err = formam.NewDecoder(&formam.DecoderOptions{
				TagName:           "json",
				IgnoreUnknownKeys: true,
			}).Decode(r.Form, &args)
		}
	}
	if err != nil {
		ferr, ok := err.(*formam.Error)
		if !ok || ferr.Code() != formam.ErrCodeConversion {
			return err
		}
		v.Append(ferr.Path(), "must be a number")

		// formam stops decoding on the first error, so there's nothing more
		// to report; bail out with what we have.
		return h.main(&v)(w, r)
	}

	site.Settings = args.Settings
	site.LinkDomain = args.LinkDomain

	err = site.Update(r.Context())
	if err != nil {
		var vErr *zvalidate.Validator
		if !errors.As(err, &vErr) {
			return err
		}
		v.Sub("site", "", err)
	}

	if v.HasErrors() {
		return h.main(&v)(w, r)
	}

	zhttp.Flash(w, r, T(r.Context(), "notify/saved|Saved!"))
	return zhttp.SeeOther(w, "/settings")
}

func (h settings) purge(w http.ResponseWriter, r *http.Request) error {
	var (
		path       = strings.TrimSpace(r.URL.Query().Get("path"))
		matchTitle = r.URL.Query().Get("match-title") == "on"
		matchCase  = r.URL.Query().Get("match-case") == "on"
		list       goatcounter.HitLists
		paths      goatcounter.Paths
	)

	if path != "" {
		err := list.ListPathsLike(r.Context(), path, matchTitle, matchCase)
		if err != nil {
			return err
		}

		_, err = paths.List(r.Context(), 0, 5_000)
		if err != nil {
			return err
		}
	}

	return zhttp.Template(w, "settings_purge.gohtml", struct {
		Globals
		PurgePath  string
		MatchTitle bool
		MatchCase  bool
		List       goatcounter.HitLists
		AllPaths   goatcounter.Paths
	}{newGlobals(w, r), path, matchTitle, matchCase, list, paths})
}

func (h settings) purgeDo(w http.ResponseWriter, r *http.Request) error {
	paths, err := zint.Split[goatcounter.PathID](r.Form.Get("paths"), ",")
	if err != nil {
		return err
	}

	ctx := context.WithoutCancel(r.Context())
	bgrun.RunFunction("purge", func() {
		var list goatcounter.Hits
		err := list.Purge(ctx, paths)
		if err != nil {
			log.Error(ctx, err)
		}
	})

	zhttp.Flash(w, r, T(r.Context(),
		"notify/started-background-process|Started in the background; may take about 10-20 seconds to fully process."))
	return zhttp.SeeOther(w, "/settings/purge")
}

func (h settings) merge(w http.ResponseWriter, r *http.Request) error {
	v := goatcounter.NewValidate(r.Context())
	pathID := goatcounter.PathID(v.Integer32("merge_with", r.Form.Get("merge_with")))
	if v.HasErrors() {
		return v
	}
	var p goatcounter.Path
	err := p.ByID(r.Context(), pathID)
	if err != nil {
		return err
	}

	mergeIDs, err := zint.Split[goatcounter.PathID](r.Form.Get("paths"), ",")
	if err != nil {
		return err
	}
	mergeIDs = slices.DeleteFunc(mergeIDs, func(p goatcounter.PathID) bool { return p == pathID })
	if len(mergeIDs) == 0 {
		return guru.New(400, T(r.Context(), "error/merge-self|Cannot merge a path with itself"))
	}
	merge := make(goatcounter.Paths, len(mergeIDs))
	for i := range mergeIDs {
		err := merge[i].ByID(r.Context(), mergeIDs[i])
		if err != nil {
			return err
		}
	}

	ctx := context.WithoutCancel(r.Context())
	bgrun.RunFunction("merge", func() {
		err := p.Merge(ctx, merge)
		if err != nil {
			log.Error(ctx, err)
		}
	})

	zhttp.Flash(w, r, T(r.Context(), `notify/started-background-process|
		Started in the background; may take about 10-20 seconds to fully process.`))
	return zhttp.SeeOther(w, "/settings/purge")
}
