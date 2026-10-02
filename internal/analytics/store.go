// Package analytics collects pageviews for the configured sites and queries
// them for the dashboard.
package analytics

import (
	"strings"
	"sync"

	"github.com/marvinrabe/goatcounter/internal/database"
)

// Store collects and queries the pageviews of the configured sites. The
// settings are set before it's used and don't change.
type Store struct {
	DB       *database.DB
	Timezone Timezone // Of the dashboard; days start at midnight here.
	Sites    []Site

	salts struct {
		mu        sync.Mutex
		day       string
		cur, prev []byte
	}
}

// Site gets a configured site by name.
func (s *Store) Site(name string) (Site, bool) {
	for _, site := range s.Sites {
		if strings.EqualFold(site.LinkDomain, strings.TrimSpace(name)) {
			return site, true
		}
	}
	return Site{}, false
}

// Query selects the pageviews of a site that the dashboard shows.
type Query struct {
	Site   Site
	Range  Range
	Filter PathFilter
}
