package enrich

import (
	"net/http/httptest"
	"testing"
)

func TestIsBot(t *testing.T) {
	browser := "Mozilla/5.0 (X11; Linux x86_64; rv:79.0) Gecko/20100101 Firefox/79.0"
	for _, tt := range []struct {
		ua, ip, purpose string
		want            bool
	}{
		{browser, "192.0.2.1", "", false},
		{browser, "3.0.0.1", "", true},        // AWS.
		{browser, "::ffff:3.0.0.1", "", true}, // AWS.
		{browser, "192.0.2.1", "prefetch", true},
		{"bot", "192.0.2.1", "", true}, // Too short.
		{"curl/8.0 (x86_64)", "192.0.2.1", "", true},
		{"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "192.0.2.1", "", true},
		{"Mozilla/5.0 (Linux; Android 9; CUBOT_X19) Mobile Safari/537.36", "192.0.2.1", "", false},
	} {
		r := httptest.NewRequest("GET", "/count", nil)
		r.Header.Set("User-Agent", tt.ua)
		r.Header.Set("Purpose", tt.purpose)
		r.RemoteAddr = tt.ip
		if have := IsBot(r); have != tt.want {
			t.Errorf("%s %s %s: %t, want %t", tt.ua, tt.ip, tt.purpose, have, tt.want)
		}
	}
}
