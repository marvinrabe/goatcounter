package goatcounter

import (
	"context"
	"path"
	"strings"

	"zgo.at/errors"
	"zgo.at/zdb"
	"zgo.at/zstd/zstring"
)

var statTables = []string{"system_stats", "browser_stats", "location_stats", "language_stats", "size_stats"}

// Site is configured through GOATCOUNTER_SITES. Its name is also the stable
// key stored with every data row.
type Site struct {
	Key        string `db:"site" json:"site"`
	LinkDomain string `db:"-" json:"link_domain"`
}

func (s *Site) Defaults() {
	s.LinkDomain = strings.TrimRight(strings.TrimSpace(s.LinkDomain), "/")
}

func (s Site) ClearCache(ctx context.Context, full bool) {
	if full {
		cachePaths(ctx).Reset()
	}
}

func (s Site) Domain(ctx context.Context) string {
	if h := Host(ctx); h != "" {
		return h
	}
	return Config(ctx).Domain
}
func (s Site) Display(context.Context) string           { return s.LinkDomain }
func (s Site) SchemelessURL(ctx context.Context) string { return s.Domain(ctx) + Config(ctx).BasePath }
func (s Site) URL(ctx context.Context) string {
	if Config(ctx).Dev {
		return "http://" + s.SchemelessURL(ctx)
	}
	return "https://" + s.SchemelessURL(ctx)
}
func (s Site) LinkDomainURL(withProto bool, paths ...string) string {
	if s.LinkDomain == "" {
		return ""
	}
	d := s.LinkDomain
	if withProto && !zstring.HasPrefixes(d, "http://", "https://") {
		d = "http://" + d
	} else if !withProto {
		d = zstring.TrimPrefixes(d, "http://", "https://")
	}
	return strings.TrimRight(d, "/") + path.Join(paths...)
}

func (s Site) DeleteAll(ctx context.Context) error {
	return zdb.TX(ctx, func(ctx context.Context) error {
		if err := clearFilters(ctx, s.Key); err != nil {
			return errors.Wrap(err, "Site.DeleteAll: clear filters")
		}
		for _, t := range append(statTables, "campaign_stats", "hit_counts", "ref_counts", "hits", "bots", "paths", "hit_queue") {
			if err := zdb.Exec(ctx, `delete from `+t+` where site=?`, s.Key); err != nil {
				return errors.Wrap(err, "Site.DeleteAll: delete "+t)
			}
		}
		if err := zdb.Exec(ctx, `delete from collector_session_paths where session in
            (select session from collector_sessions where site=?)`, s.Key); err != nil {
			return err
		}
		if err := zdb.Exec(ctx, `delete from collector_sessions where site=?`, s.Key); err != nil {
			return err
		}
		s.ClearCache(ctx, true)
		return nil
	})
}
