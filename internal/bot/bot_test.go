package bot

import (
	"net/http/httptest"
	"testing"
)

func TestCollectorClassification(t *testing.T) {
	browser := "Mozilla/5.0 (X11; Linux x86_64; rv:79.0) Gecko/20100101 Firefox/79.0"
	for _, tt := range []struct {
		ua, ip, purpose string
		want            Result
	}{
		{browser, "192.0.2.1", "", NoBotNoMatch},
		{browser, "3.0.0.1", "", 8},
		{browser, "::ffff:3.0.0.1", "", 8},
		{browser, "192.0.2.1", "prefetch", BotPrefetch},
		{"bot", "192.0.2.1", "", BotShort},
		{"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "192.0.2.1", "", BotLink},
	} {
		r := httptest.NewRequest("GET", "/count", nil)
		r.Header.Set("User-Agent", tt.ua)
		r.Header.Set("Purpose", tt.purpose)
		r.RemoteAddr = tt.ip
		if got := Bot(r); got != tt.want {
			t.Errorf("%s %s %s: %d, want %d", tt.ua, tt.ip, tt.purpose, got, tt.want)
		}
	}
}
