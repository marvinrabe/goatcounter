package i18n

import (
	"context"
	"testing"
)

func TestT(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name, in string
		data     []any
		want     string
	}{
		{"no params", "id|Hello", nil, "Hello"},
		{"one var", "id|Hello, %(name).", []any{"Martin"}, "Hello, Martin."},
		{
			// Regression: each replacement changes the length of the string, so
			// replacing front to back used stale indexes and panicked.
			"multiple vars",
			"id|all times are in %(tz-name) (%(tz-offset)).",
			[]any{P{"tz-name": "UTC", "tz-offset": "+00:00"}},
			"all times are in UTC (+00:00).",
		},
		{
			"multiple tags",
			"id|%[%a one] and %[%b two] and %(three)",
			[]any{P{
				"a":     Tag("em", ""),
				"b":     Tag("strong", ""),
				"three": "3",
			}},
			"<em>one</em> and <strong>two</strong> and 3",
		},
		{"escapes html", "id|Hello, %(name).", []any{"<b>"}, "Hello, &lt;b&gt;."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			have := T(ctx, tt.in, tt.data...)
			if have != tt.want {
				t.Errorf("\nhave: %q\nwant: %q", have, tt.want)
			}
		})
	}
}
