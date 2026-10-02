package enrich

import "testing"

func TestSource(t *testing.T) {
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
		source, referrer := Source(tt.in, "example.com")
		if source != tt.source || referrer != tt.referrer {
			t.Errorf("%q: have (%q, %q); want (%q, %q)", tt.in, source, referrer, tt.source, tt.referrer)
		}
	}
}
