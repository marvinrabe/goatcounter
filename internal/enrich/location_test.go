package enrich

import (
	"net/http/httptest"
	"testing"
)

func TestCountry(t *testing.T) {
	for _, tt := range []struct {
		header, value string
		want          string
	}{
		{"", "DE", ""}, // Not configured: the header is ignored.
		{"CDN-RequestCountryCode", "DE", "DE"},
		{"CDN-RequestCountryCode", " si ", "SI"},
		{"CDN-RequestCountryCode", "XX", ""}, // Unknown codes aren't stored.
		{"CDN-RequestCountryCode", "", ""},
	} {
		r := httptest.NewRequest("GET", "/count", nil)
		r.Header.Set("CDN-RequestCountryCode", tt.value)
		g := &Geo{Header: tt.header}
		if have := g.Country(r); have != tt.want {
			t.Errorf("%q: %q: have %q; want %q", tt.header, tt.value, have, tt.want)
		}
	}

	var g *Geo
	if have := g.Country(httptest.NewRequest("GET", "/count", nil)); have != "" {
		t.Errorf("nil Geo: %q", have)
	}
	if have := CountryName("SI"); have != "Slovenia" {
		t.Errorf("CountryName: %q", have)
	}
}
