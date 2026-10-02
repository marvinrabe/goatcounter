package enrich

import "testing"

func TestParseUserAgent(t *testing.T) {
	for ua, want := range map[string]UserAgent{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.2210.91":       {"Microsoft Edge", "120.0", "Windows", "Desktop"},
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15":                   {"Safari", "17.5", "Mac", "Desktop"},
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1": {"Safari", "17.4", "iOS", "Mobile"},
		"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0":                                                          {"Firefox", "120.0", "Ubuntu", "Desktop"},
		"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36":                         {"Chrome", "120.0", "Android", "Mobile"},
		"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36":                                {"Chrome", "120.0", "Android", "Tablet"},
		"Mozilla/5.0 (iPad; CPU OS 17_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.6 Mobile/15E148 Safari/604.1":          {"Safari", "17.6", "iPadOS", "Tablet"},
		"Mozilla/5.0 (Android 14; Mobile; rv:131.0) Gecko/131.0 Firefox/131.0":                                                                    {"Firefox", "131.0", "Android", "Mobile"},
		"Mozilla/5.0 (Android 14; Tablet; rv:131.0) Gecko/131.0 Firefox/131.0":                                                                    {"Firefox", "131.0", "Android", "Tablet"},
		"Mozilla/5.0 (X11; Fedora; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/119.0":                                                          {"Firefox", "119.0", "Fedora", "Desktop"},
		"Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36":                          {"Chrome", "120.0", "Chrome OS", "Desktop"},
		"Mozilla/5.0 (Windows NT 6.1; Win64; x64; rv:115.0) Gecko/20100101 Firefox/115.0":                                                         {"Firefox", "115.0", "Windows", "Desktop"},
		"Mozilla/5.0 (Linux; Android 8.1.0; SM-T580) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36":                       {"Chrome", "120.0", "Android", "Tablet"},
		"Mozilla/5.0 (SMART-TV; Linux; Tizen 4.0) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/2.1 Chrome/56.0.2924.0 TV Safari/537.36":  {"Samsung Browser", "2.1", "GNU/Linux", "TV"},
		"curl/8.4.0": {},
	} {
		if have := ParseUserAgent(ua); have != want {
			t.Errorf("%s\nhave: %+v\nwant: %+v", ua, have, want)
		}
	}
}
