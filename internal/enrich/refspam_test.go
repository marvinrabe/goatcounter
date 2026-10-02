package enrich

import (
	"testing"
)

func TestIsSpam(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"notinthelist.com", false},
		{"foo.notinthelist.com", false},

		{"localhost", true},
		{"a.localhost", true},
		{"c.a.localhost", true},

		{"adcash.com", true},
		{"d.adcash.com", true},

		{"dadcash.com", false},
		{"localhost.com", false},
		{"asdlocalhost.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := isSpam(tt.in)
			if got != tt.want {
				t.Errorf("\ngot:  %t\nwant: %t", got, tt.want)
			}
		})
	}
}

func BenchmarkIsSpam(b *testing.B) {
	isSpam("notinthelist.com")

	b.ReportAllocs()
	b.ResetTimer()
	v := false
	for n := 0; n < b.N; n++ {
		v = isSpam("notinthelist.com")
	}
	_ = v
}
