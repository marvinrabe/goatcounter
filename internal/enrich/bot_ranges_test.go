package enrich

import (
	"fmt"
	"io"
	"math/rand/v2"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestRanges(t *testing.T) {
	r := NewRanges([]netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/24"),
		netip.MustParsePrefix("10.0.0.128/25"), // Inside the one before.
		netip.MustParsePrefix("10.0.1.0/24"),   // Adjacent.
		netip.MustParsePrefix("192.0.2.7/32"),
		netip.MustParsePrefix("255.255.255.0/24"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2001:db8:1::/48"),
		netip.MustParsePrefix("::ffff:198.51.100.0/120"), // Mapped IPv4.
		netip.MustParsePrefix("ffff:ffff:ffff:ffff::/64"),
	})
	if r.Len() != 6 {
		t.Errorf("Len = %d; want 6", r.Len())
	}
	for ip, want := range map[string]bool{
		"9.255.255.255":          false,
		"10.0.0.0":               true,
		"10.0.1.255":             true,
		"10.0.2.0":               false,
		"192.0.2.6":              false,
		"192.0.2.7":              true,
		"192.0.2.8":              false,
		"255.255.255.255":        true,
		"198.51.100.1":           true,
		"::ffff:10.0.0.1":        true,
		"2001:db7:ffff::1":       false,
		"2001:db8::1":            true,
		"2001:db8:ffff:ffff::1":  true,
		"2001:db9::":             false,
		"ffff:ffff:ffff:ffff::1": true,
		"ffff:ffff:ffff:fffe::1": false,
		"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff": true,
	} {
		if have := r.Contains(netip.MustParseAddr(ip)); have != want {
			t.Errorf("%s: %t; want %t", ip, have, want)
		}
	}
	if (Ranges{}).Contains(netip.MustParseAddr("10.0.0.1")) || r.Contains(netip.Addr{}) {
		t.Error("empty")
	}
}

// The merged spans must give the same answer as checking every prefix.
func TestRangesSnapshot(t *testing.T) {
	var prefixes []netip.Prefix
	for _, record := range strings.Fields(cloudSnapshot) {
		cidr, _, _ := strings.Cut(record, ",")
		prefixes = append(prefixes, netip.MustParsePrefix(cidr))
	}
	r := NewRanges(prefixes)

	rnd := rand.New(rand.NewPCG(1, 2))
	for range 20_000 {
		// Half near a network from the list, half anywhere.
		p := prefixes[rnd.IntN(len(prefixes))]
		b := p.Addr().As16()
		if p.Addr().Is4() {
			b4 := p.Addr().As4()
			if rnd.IntN(2) == 0 {
				b4[3] ^= byte(rnd.IntN(256))
				b4[2] ^= byte(rnd.IntN(4))
			} else {
				b4 = [4]byte{byte(rnd.IntN(256)), byte(rnd.IntN(256)), byte(rnd.IntN(256)), byte(rnd.IntN(256))}
			}
			check(t, r, prefixes, netip.AddrFrom4(b4))
			continue
		}
		b[rnd.IntN(16)] ^= byte(rnd.IntN(256))
		check(t, r, prefixes, netip.AddrFrom16(b))
	}
}

func check(t *testing.T, r Ranges, prefixes []netip.Prefix, ip netip.Addr) {
	t.Helper()
	want := false
	for _, p := range prefixes {
		if p.Contains(ip) {
			want = true
			break
		}
	}
	if have := r.Contains(ip); have != want {
		t.Fatalf("%s: %t; want %t", ip, have, want)
	}
}

func TestParse(t *testing.T) {
	var aws, gcp, oracle, feed strings.Builder
	aws.WriteString(`{"prefixes": [{"ip_prefix": "52.95.0.0/16", "service": "AMAZON"}`)
	gcp.WriteString(`{"prefixes": [{"ipv6Prefix": "2600:1900::/35", "service": "Google Cloud"}`)
	oracle.WriteString(`{"regions": [{"region": "x", "cidrs": [{"cidr": "129.80.0.0/16", "tags": ["OCI"]}`)
	feed.WriteString("# A comment\n\n")
	for i := range minPrefixes {
		fmt.Fprintf(&aws, `, {"ip_prefix": "3.%d.0.0/16", "service": "EC2"}`, i)
		fmt.Fprintf(&gcp, `, {"ipv4Prefix": "34.%d.0.0/16"}`, i)
		fmt.Fprintf(&oracle, `, {"cidr": "130.%d.0.0/16"}`, i)
		fmt.Fprintf(&feed, "5.%d.0.0/16,NL,NL-NH,Amsterdam,\n", i)
	}
	aws.WriteString(`], "ipv6_prefixes": [{"ipv6_prefix": "2600:1f00::/24", "service": "EC2"}]}`)
	gcp.WriteString(`]}`)
	oracle.WriteString(`]}]}`)

	for _, tt := range []struct {
		name  string
		parse func(string) (Ranges, error)
		in    string
		yes   []string
		no    []string
	}{
		{"aws", s(parseAWS), aws.String(), []string{"3.0.0.1", "3.19.255.255", "2600:1f00::1"}, []string{"52.95.0.1", "3.20.0.0"}},
		{"gcp", s(parseGoogleCloud), gcp.String(), []string{"34.1.2.3", "2600:1900::1"}, []string{"34.20.0.0"}},
		{"oracle", s(parseOracle), oracle.String(), []string{"129.80.0.1", "130.3.0.1"}, []string{"130.20.0.1"}},
		{"geofeed", s(parseGeofeed), feed.String(), []string{"5.0.0.1"}, []string{"5.20.0.1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, err := tt.parse(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			for _, ip := range tt.yes {
				if !r.Contains(netip.MustParseAddr(ip)) {
					t.Errorf("%s not in ranges", ip)
				}
			}
			for _, ip := range tt.no {
				if r.Contains(netip.MustParseAddr(ip)) {
					t.Errorf("%s in ranges", ip)
				}
			}
			if _, err := tt.parse(tt.in[:len(tt.in)/2]); err == nil {
				t.Error("no error for a truncated list")
			}
		})
	}

	if _, err := parseGeofeed(strings.NewReader("5.0.0.0/16\n")); err == nil {
		t.Error("no error for a short list")
	}
}

func s(f func(io.Reader) (Ranges, error)) func(string) (Ranges, error) {
	return func(in string) (Ranges, error) { return f(strings.NewReader(in)) }
}

func BenchmarkIsBot(b *testing.B) {
	r := httptest.NewRequest("GET", "/count", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:79.0) Gecko/20100101 Firefox/79.0")
	r.RemoteAddr = "192.0.2.1"
	b.ReportAllocs()
	for b.Loop() {
		IsBot(r)
	}
}
