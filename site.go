package goatcounter

import (
	"context"
	"path"
	"strings"
	"time"

	"github.com/marvinrabe/goatcounter/internal/db2"
	"zgo.at/errors"
	"zgo.at/zdb"
	"zgo.at/zstd/zbool"
	"zgo.at/zstd/zslice"
	"zgo.at/zstd/zstring"
	"zgo.at/zstd/ztime"
)

var statTables = []string{"system_stats", "browser_stats", "location_stats", "language_stats", "size_stats"}

// Site is the one and only site this installation tracks; it's stored as a
// single row in the "site" table and mostly holds settings.
type Site struct {
	// Site domain for linking (www.arp242.net). Note this can be a full URL and
	// is a bit misnamed.
	LinkDomain string `db:"link_domain" json:"link_domain"`

	Settings SiteSettings `db:"settings" json:"settings"`

	// Whether this site has received any data; will be true after the first
	// pageview.
	ReceivedData zbool.Bool `db:"received_data" json:"received_data"`

	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt  *time.Time `db:"updated_at" json:"updated_at"`
	FirstHitAt time.Time  `db:"first_hit_at" json:"first_hit_at"`
}

func (Site) Table() string { return "site" }

var _ zdb.Defaulter = &Site{}

func (s *Site) Defaults(ctx context.Context) {
	n := ztime.Now(ctx)
	if s.CreatedAt.IsZero() {
		s.CreatedAt = n
	} else {
		s.UpdatedAt = &n
	}
	if s.FirstHitAt.IsZero() {
		s.FirstHitAt = n
	}

	s.LinkDomain = strings.TrimRight(s.LinkDomain, "/")

	s.Settings.Defaults(ctx)
}

var _ zdb.Validator = &Site{}

func (s *Site) Validate(ctx context.Context) error {
	v := NewValidate(ctx)
	v.URL("link_domain", s.LinkDomain)
	v.Sub("settings", "", s.Settings.Validate(ctx))
	return v.ErrorOrNil()
}

// Load the site, creating it with the default settings if it doesn't exist yet.
func (s *Site) Load(ctx context.Context) error {
	if c, ok := cacheSite(ctx).Get(cacheSiteKey); ok {
		*s = *c
		return nil
	}

	err := zdb.Get(ctx, s, `/* Site.Load */ select * from site limit 1`)
	if zdb.ErrNoRows(err) {
		s.Defaults(ctx)
		err = s.insert(ctx)
	}
	if err != nil {
		return errors.Wrap(err, "Site.Load")
	}

	// Fill in anything the stored JSON doesn't have yet; settings added in a
	// later version won't be in a row written by an earlier one.
	s.Settings.Defaults(ctx)

	s.cache(ctx)
	return nil
}

func (s Site) cache(ctx context.Context) {
	c := s
	cacheSite(ctx).Set(cacheSiteKey, &c)
}

// ClearCache clears the cache for this site.
func (s Site) ClearCache(ctx context.Context, full bool) {
	cacheSite(ctx).Delete(cacheSiteKey)

	if full {
		cachePaths(ctx).Reset()
		cacheChangedTitles(ctx).Reset()
	}
}

func (s *Site) insert(ctx context.Context) error {
	err := zdb.Exec(ctx, `insert into site
		(link_domain, settings, received_data, created_at, first_hit_at)
		values (?, ?, ?, ?, ?)`,
		s.LinkDomain, s.Settings, s.ReceivedData, s.CreatedAt, s.FirstHitAt)
	return errors.Wrap(err, "Site.insert")
}

func (s *Site) Update(ctx context.Context) error {
	s.Defaults(ctx)
	if err := s.Validate(ctx); err != nil {
		return err
	}

	err := zdb.Exec(ctx, `update site set
		link_domain=?, settings=?, updated_at=?`,
		s.LinkDomain, s.Settings, s.UpdatedAt)
	if err != nil {
		return errors.Wrap(err, "Site.Update")
	}

	s.ClearCache(ctx, false)
	return nil
}

func (s *Site) UpdateReceivedData(ctx context.Context) error {
	s.ReceivedData = true
	err := zdb.Exec(ctx, `update site set received_data=1`)
	s.ClearCache(ctx, false)
	return errors.Wrap(err, "Site.UpdateReceivedData")
}

func (s *Site) UpdateFirstHitAt(ctx context.Context, f time.Time) error {
	s.FirstHitAt = f.UTC().Add(-12 * time.Hour)
	err := zdb.Exec(ctx, `update site set first_hit_at=?`, s.FirstHitAt)
	s.ClearCache(ctx, false)
	return errors.Wrap(err, "Site.UpdateFirstHitAt")
}

// Domain this site is served from.
func (s Site) Domain(ctx context.Context) string {
	if h := Host(ctx); h != "" {
		return h
	}
	return Config(ctx).Domain
}

// Display format: just the domain.
func (s Site) Display(ctx context.Context) string { return s.Domain(ctx) }

// SchemelessURL is the URL to this site, without the scheme.
func (s Site) SchemelessURL(ctx context.Context) string {
	return s.Domain(ctx) + Config(ctx).BasePath
}

// URL to this site.
func (s Site) URL(ctx context.Context) string {
	if Config(ctx).Dev {
		return "http://" + s.SchemelessURL(ctx)
	}
	return "https://" + s.SchemelessURL(ctx)
}

// LinkDomainURL creates a valid url to the configured LinkDomain.
func (s Site) LinkDomainURL(withProto bool, paths ...string) string {
	if s.LinkDomain == "" {
		return ""
	}
	if withProto && !zstring.HasPrefixes(s.LinkDomain, "http://", "https://") {
		s.LinkDomain = "http://" + s.LinkDomain
	} else if !withProto {
		s.LinkDomain = zstring.TrimPrefixes(s.LinkDomain, "http://", "https://")
	}
	return strings.TrimRight(s.LinkDomain, "/") + path.Join(paths...)
}

// DeleteAll deletes all pageviews, keeping the settings and users intact.
func (s Site) DeleteAll(ctx context.Context) error {
	return zdb.TX(ctx, func(ctx context.Context) error {
		for _, t := range append(statTables, "campaign_stats", "hit_counts", "ref_counts", "hits", "paths") {
			err := zdb.Exec(ctx, `delete from `+t)
			if err != nil {
				return errors.Wrap(err, "Site.DeleteAll: delete "+t)
			}
		}

		s.ClearCache(ctx, true)
		return nil
	})
}

func (s Site) DeleteOlderThan(ctx context.Context, days int) error {
	if days < 14 {
		return errors.Errorf("days must be at least 14: %d", days)
	}

	return zdb.TX(ctx, func(ctx context.Context) error {
		ival := Interval(ctx, days)

		var pathIDs []PathID
		err := zdb.Select(ctx, &pathIDs, `/* Site.DeleteOlderThan */
			select path_id from hit_counts where hour < `+ival+` group by path_id`)
		if err != nil {
			return errors.Wrap(err, "Site.DeleteOlderThan: get paths")
		}

		for _, t := range append(statTables, "campaign_stats") {
			err := zdb.Exec(ctx, `delete from `+t+` where day < `+ival)
			if err != nil {
				return errors.Wrap(err, "Site.DeleteOlderThan: delete "+t)
			}
		}

		for _, t := range []string{"hit_counts", "ref_counts"} {
			err := zdb.Exec(ctx, `delete from `+t+` where hour < `+ival)
			if err != nil {
				return errors.Wrap(err, "Site.DeleteOlderThan: delete "+t)
			}
		}

		err = zdb.Exec(ctx, `/* Site.DeleteOlderThan */
			delete from hits where created_at < `+ival)
		if err != nil {
			return errors.Wrap(err, "Site.DeleteOlderThan: delete hits")
		}

		if len(pathIDs) > 0 {
			var remainPath []PathID
			err := zdb.Select(ctx, &remainPath, `/* Site.DeleteOlderThan */
				select path_id from hit_counts where path_id :in (:paths)`,
				map[string]any{
					"paths": db2.Array(ctx, pathIDs),
					"in":    db2.In(ctx),
				})
			if err != nil {
				return errors.Wrap(err, "Site.DeleteOlderThan")
			}

			diff := zslice.Difference(pathIDs, remainPath)
			if len(diff) > 0 {
				err = zdb.Exec(ctx, `/* Site.DeleteOlderThan */
					delete from paths where path_id :in (:paths)`,
					map[string]any{
						"paths": db2.Array(ctx, diff),
						"in":    db2.In(ctx),
					})
				if err != nil {
					return errors.Wrap(err, "Site.DeleteOlderThan")
				}
			}
		}

		return nil
	})
}
