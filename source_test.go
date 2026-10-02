package goatcounter

import "testing"

func TestReferrerSource(t *testing.T) {
	for _, tt := range []struct{ in, source, referrer string }{
		{"", "", ""},
		{"https://www.google.com/", "Google", "google.com"},
		{"https://www.google.co.uk/url?q=x", "Google", "google.co.uk/url"},
		{"https://lens.google.com/", "Google", "lens.google.com"},
		{"https://cn.bing.com/search?q=x", "Bing", "cn.bing.com/search"},
		{"https://de.indeed.com/jobs", "Indeed", "de.indeed.com/jobs"},
		{"https://t.co/abc", "X (Twitter)", "t.co/abc"},
		{"https://statics.teams.cdn.office.net/x", "Microsoft Teams", "statics.teams.cdn.office.net/x"},
		{"https://www.roll-pastuch.de/de/", "roll-pastuch.de", "roll-pastuch.de/de"},
		{"android-app://com.linkedin.android/", "LinkedIn", "android-app://com.linkedin.android"},
		{"https://example.com/page", "", ""},     // Internal.
		{"https://www.example.com/page", "", ""}, // Internal.
		{"https://sub.example.com/page", "", ""}, // Internal.
		{"xx:", "", ""},
	} {
		source, referrer := referrerSource(tt.in, "example.com")
		if source != tt.source || referrer != tt.referrer {
			t.Errorf("%q: have (%q, %q); want (%q, %q)", tt.in, source, referrer, tt.source, tt.referrer)
		}
	}
}

func TestParseUserAgent(t *testing.T) {
	for ua, want := range map[string]userAgent{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.2210.91":       {"Microsoft Edge", "120.0", "Windows", "10"},
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15":                   {"Safari", "17.5", "Mac", "10.15"},
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1": {"Safari", "17.4", "iOS", "17.4"},
		"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0":                                                          {"Firefox", "120.0", "Ubuntu", ""},
		"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36":                         {"Chrome", "120.0", "Android", "10"},
	} {
		if have := parseUserAgent(ua); have != want {
			t.Errorf("%s\nhave: %+v\nwant: %+v", ua, have, want)
		}
	}
}
