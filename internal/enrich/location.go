package enrich

import (
	"net/http"
	"strings"
)

// Country gets the ISO 3166-1 alpha-2 code of the country a request is from,
// from the CDN's CountryHeader. It's "" if it's unknown.
func Country(r *http.Request) string {
	if c := strings.ToUpper(strings.TrimSpace(r.Header.Get(CountryHeader))); countryNames[c] != "" {
		return c
	}
	return ""
}

// CountryName gets the English name for a country code, or "" if it's
// unknown.
func CountryName(code string) string { return countryNames[code] }
