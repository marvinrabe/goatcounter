package goatcounter

import "testing"

func TestAcceptLanguage(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"*", ""},
		{"xx", ""},                   // Not a language we know.
		{"nl", "nld"},                // ISO 639-1.
		{"NL", "nld"},                // Case-insensitive.
		{"nl-BE", "nld"},             // Region is dropped.
		{" nl-BE ", "nld"},           // Spaces around the tag.
		{"ceb", "ceb"},               // No two-letter code, sent as-is.
		{"iw", "heb"},                // Deprecated code.
		{"nl,en", "nld"},             // First one wins if there's no q.
		{"nl;q=0.8,en", "eng"},       // Highest q wins, in any order.
		{"en,nl;q=0.8", "eng"},       //
		{"nl;q=0.8,en;q=0.9", "eng"}, //
		{"nl;q=0.9,en;q=0.8", "nld"}, //
		{"en;q=0, nl", "nld"},        // q=0 means "not acceptable".
		{"nl;q=huh, en", "eng"},      // Ignore tags we can't parse.
		{"*,nl", "nld"},              //
		{"xx;q=0.9,nl;q=0.8", "nld"}, // Skip over unknown languages.
		{"en-US,en;q=0.9,nl;q=0.8", "eng"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			have := ""
			if l := AcceptLanguage(tt.in); l != nil {
				have = *l
			}
			if have != tt.want {
				t.Errorf("\nhave: %q\nwant: %q", have, tt.want)
			}
		})
	}
}
