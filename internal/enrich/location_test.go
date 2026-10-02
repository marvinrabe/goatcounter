package enrich

import (
	"net/http/httptest"
	"testing"
)

func TestCountry(t *testing.T) {
	for _, tt := range []struct {
		value, want string
	}{
		{"DE", "DE"},
		{" si ", "SI"},
		{"XX", ""}, // Unknown codes aren't stored.
		{"", ""},
	} {
		r := httptest.NewRequest("GET", "/count", nil)
		r.Header.Set(CountryHeader, tt.value)
		if have := Country(r); have != tt.want {
			t.Errorf("%q: have %q; want %q", tt.value, have, tt.want)
		}
	}
	if have := CountryName("SI"); have != "Slovenia" {
		t.Errorf("CountryName: %q", have)
	}
}
