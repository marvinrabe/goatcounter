package goatcounter

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/marvinrabe/goatcounter/internal/database"
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

func (s Site) LinkDomainURL(withProto bool, paths ...string) string {
	if s.LinkDomain == "" {
		return ""
	}
	d := s.LinkDomain
	if withProto && !(strings.HasPrefix(d, "http://") || strings.HasPrefix(d, "https://")) {
		d = "http://" + d
	} else if !withProto {
		d = strings.TrimPrefix(strings.TrimPrefix(d, "http://"), "https://")
	}
	return strings.TrimRight(d, "/") + path.Join(paths...)
}

func (s Site) DeleteAll(ctx context.Context) error {
	return database.TX(ctx, func(ctx context.Context) error {
		if err := clearFilters(ctx, s.Key); err != nil {
			if err != nil {
				err = fmt.Errorf("Site.DeleteAll: clear filters: %w", err)
			}
			return err
		}
		for _, t := range append(statTables, "campaign_stats", "hit_counts", "ref_counts", "hits", "bots", "paths", "hit_queue") {
			if err := database.Exec(ctx, `delete from `+t+` where site=?`, s.Key); err != nil {
				if err != nil {
					err = fmt.Errorf("%s: %w", "Site.DeleteAll: delete "+t, err)
				}
				return err
			}
		}
		if err := database.Exec(ctx, `delete from collector_session_paths where session in
            (select session from collector_sessions where site=?)`, s.Key); err != nil {
			return err
		}
		if err := database.Exec(ctx, `delete from collector_sessions where site=?`, s.Key); err != nil {
			return err
		}
		clear(batchCacheFor(ctx).paths)
		return nil
	})
}
