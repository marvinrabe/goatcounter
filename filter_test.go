package goatcounter_test

import (
	"testing"

	. "github.com/marvinrabe/goatcounter"
)

func TestFilterMatch(t *testing.T) {
	tests := []struct {
		query, path string
		event, want bool
	}{
		{"", "/hello", false, true},
		{"e", "/hello", false, true},
		{"/h in:path", "/hello", false, true},
		{", world in:path", "/hello", false, false},
		{"/h in:path at:end", "/hello", false, false},
		{"/h in:path at:start", "/hello", false, true},
		{"/h in:path at:start at:end", "/hello", false, false},
		{"/hello in:path at:start at:end", "/hello", false, true},

		{"HELLO", "/hello", false, true},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			have := Filter{Query: tt.query}.Match(tt.path, tt.event)
			if have != tt.want {
				t.Error(tt.query)
			}

			have = Filter{Query: tt.query + " :not"}.Match(tt.path, tt.event)
			if have == tt.want {
				t.Error(":NOT →", tt.query)
			}
		})
	}
}
