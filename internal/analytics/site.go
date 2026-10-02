package analytics

import (
	"path"
	"strings"
)

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
