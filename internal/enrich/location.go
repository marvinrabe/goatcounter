package enrich

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"github.com/oschwald/geoip2-golang/v2"
)

// Geo finds the visitor's country. The zero value, and nil, finds nothing.
type Geo struct {
	// Request header with the visitor's country code, as set by a CDN in
	// front: "CDN-RequestCountryCode" for bunny.net.
	Header string
	// GeoIP database, for requests without the header.
	DB *geoip2.Reader
}

// OpenGeoDB opens the Country or City mmdb file at path.
func OpenGeoDB(path string) (*geoip2.Reader, error) {
	if strings.HasPrefix(path, "maxmind:") {
		return nil, fmt.Errorf("automatic downloads are unsupported; download a GeoIP database and set its path")
	}
	return geoip2.Open(path)
}

// Country gets the ISO 3166-1 alpha-2 code of the country a request is from:
// from the CDN's header if it's sent, or else from the GeoIP database. It's ""
// if it's unknown.
func (g *Geo) Country(r *http.Request) string {
	if g == nil {
		return ""
	}
	if g.Header != "" {
		if c := strings.ToUpper(strings.TrimSpace(r.Header.Get(g.Header))); countryNames[c] != "" {
			return c
		}
	}
	if g.DB == nil {
		return ""
	}
	ip, err := netip.ParseAddr(r.RemoteAddr)
	if err != nil {
		return ""
	}
	loc, err := g.DB.Country(ip)
	if err != nil {
		return ""
	}
	return loc.Country.ISOCode
}

// CountryName gets the English name for a country code, or "" if it's
// unknown.
func CountryName(code string) string { return countryNames[code] }
