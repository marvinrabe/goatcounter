package enrich

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"slices"
	"strings"
)

// Ranges is a set of IP networks, stored as sorted and merged address spans so
// that a lookup is a binary search.
type Ranges struct {
	v4 []span[v4]
	v6 []span[v6]
}

type (
	v4 uint32
	v6 struct{ hi, lo uint64 }

	address[T any] interface {
		comparable
		less(T) bool
		next() (T, bool) // false at the last address.
	}
	span[T address[T]] struct{ first, last T }
)

func (a v4) less(b v4) bool   { return a < b }
func (a v4) next() (v4, bool) { return a + 1, a != ^v4(0) }
func (a v6) less(b v6) bool   { return a.hi < b.hi || (a.hi == b.hi && a.lo < b.lo) }
func (a v6) next() (v6, bool) {
	n := v6{a.hi, a.lo + 1}
	if n.lo == 0 {
		n.hi++
	}
	return n, a != v6{^uint64(0), ^uint64(0)}
}

func toV6(ip netip.Addr) v6 {
	b := ip.As16()
	return v6{binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:])}
}

// NewRanges creates a set from the prefixes, which may overlap.
func NewRanges(prefixes []netip.Prefix) Ranges {
	var r Ranges
	for _, p := range prefixes {
		if p.Addr().Is4In6() && p.Bits() >= 96 {
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}
		p = p.Masked()
		host := p.Addr().BitLen() - p.Bits()
		if p.Addr().Is4() {
			first := v4(binary.BigEndian.Uint32(p.Addr().AsSlice()))
			r.v4 = append(r.v4, span[v4]{first, first | v4(uint64(1)<<host-1)})
			continue
		}
		first, last := toV6(p.Addr()), toV6(p.Addr())
		if host >= 64 {
			last.lo = ^uint64(0)
			last.hi |= uint64(1)<<(host-64) - 1
		} else {
			last.lo |= uint64(1)<<host - 1
		}
		r.v6 = append(r.v6, span[v6]{first, last})
	}
	r.v4, r.v6 = merge(r.v4), merge(r.v6)
	return r
}

// merge sorts the spans and joins those that overlap or are adjacent.
func merge[T address[T]](s []span[T]) []span[T] {
	slices.SortFunc(s, func(a, b span[T]) int {
		if a.first.less(b.first) {
			return -1
		}
		if b.first.less(a.first) {
			return 1
		}
		return 0
	})
	out := s[:0]
	for _, v := range s {
		if n := len(out); n > 0 {
			l := &out[n-1]
			after, ok := l.last.next()
			if !ok || !after.less(v.first) { // Overlaps or adjacent.
				if l.last.less(v.last) {
					l.last = v.last
				}
				continue
			}
		}
		out = append(out, v)
	}
	return slices.Clip(out)
}

func contains[T address[T]](s []span[T], ip T) bool {
	// The first span that starts after ip; only the one before it may
	// contain ip.
	i, _ := slices.BinarySearchFunc(s, ip, func(v span[T], ip T) int {
		if ip.less(v.first) {
			return 1
		}
		return -1
	})
	return i > 0 && !s[i-1].last.less(ip)
}

// Contains reports whether ip is in one of the networks.
func (r Ranges) Contains(ip netip.Addr) bool {
	ip = ip.Unmap()
	switch {
	case ip.Is4():
		return contains(r.v4, v4(binary.BigEndian.Uint32(ip.AsSlice())))
	case ip.Is6():
		return contains(r.v6, toV6(ip))
	}
	return false
}

// Len is the number of merged spans.
func (r Ranges) Len() int { return len(r.v4) + len(r.v6) }

// minPrefixes is the fewest networks an upstream list must have; anything less
// is taken to be a broken download rather than a provider that shrank.
const minPrefixes = 20

func checked(prefixes []netip.Prefix) (Ranges, error) {
	if len(prefixes) < minPrefixes {
		return Ranges{}, fmt.Errorf("only %d networks; expected at least %d", len(prefixes), minPrefixes)
	}
	return NewRanges(prefixes), nil
}

func parsePrefixes(list ...string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(list))
	for _, s := range list {
		if s == "" {
			continue
		}
		p, err := netip.ParsePrefix(strings.TrimSpace(s))
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, p)
	}
	return prefixes, nil
}

// parseAWS reads ip-ranges.json, keeping only EC2: the ranges customers run
// servers in. The rest are Amazon's own services, such as CloudFront.
func parseAWS(r io.Reader) (Ranges, error) {
	var data struct {
		V4 []struct {
			Prefix  string `json:"ip_prefix"`
			Service string `json:"service"`
		} `json:"prefixes"`
		V6 []struct {
			Prefix  string `json:"ipv6_prefix"`
			Service string `json:"service"`
		} `json:"ipv6_prefixes"`
	}
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		return Ranges{}, err
	}
	var list []string
	for _, p := range data.V4 {
		if p.Service == "EC2" {
			list = append(list, p.Prefix)
		}
	}
	for _, p := range data.V6 {
		if p.Service == "EC2" {
			list = append(list, p.Prefix)
		}
	}
	prefixes, err := parsePrefixes(list...)
	if err != nil {
		return Ranges{}, err
	}
	return checked(prefixes)
}

// parseGoogleCloud reads cloud.json.
func parseGoogleCloud(r io.Reader) (Ranges, error) {
	var data struct {
		Prefixes []struct {
			V4 string `json:"ipv4Prefix"`
			V6 string `json:"ipv6Prefix"`
		} `json:"prefixes"`
	}
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		return Ranges{}, err
	}
	list := make([]string, 0, len(data.Prefixes))
	for _, p := range data.Prefixes {
		list = append(list, p.V4, p.V6)
	}
	prefixes, err := parsePrefixes(list...)
	if err != nil {
		return Ranges{}, err
	}
	return checked(prefixes)
}

// parseOracle reads public_ip_ranges.json.
func parseOracle(r io.Reader) (Ranges, error) {
	var data struct {
		Regions []struct {
			CIDRs []struct {
				CIDR string `json:"cidr"`
			} `json:"cidrs"`
		} `json:"regions"`
	}
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		return Ranges{}, err
	}
	var list []string
	for _, region := range data.Regions {
		for _, c := range region.CIDRs {
			list = append(list, c.CIDR)
		}
	}
	prefixes, err := parsePrefixes(list...)
	if err != nil {
		return Ranges{}, err
	}
	return checked(prefixes)
}

// parseGeofeed reads an RFC 8805 geofeed: CSV with the network in the first
// column, and comments starting with "#".
func parseGeofeed(r io.Reader) (Ranges, error) {
	var prefixes []netip.Prefix
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		network, _, _ := strings.Cut(line, ",")
		p, err := netip.ParsePrefix(strings.TrimSpace(network))
		if err != nil {
			return Ranges{}, err
		}
		prefixes = append(prefixes, p)
	}
	if err := scan.Err(); err != nil {
		return Ranges{}, err
	}
	return checked(prefixes)
}
