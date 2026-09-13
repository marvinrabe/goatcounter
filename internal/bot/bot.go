// Package bot classifies collector traffic, preserving stored reason codes.
package bot

import (
	_ "embed"
	"net/http"
	"net/netip"
	"strings"
)

type Result uint8

const (
	NoBotKnown       = 0
	NoBotNoMatch     = 1
	BotPrefetch      = 2
	BotLink          = 3
	BotClientLibrary = 4
	BotKnownBot      = 5
	BotBoty          = 6
	BotShort         = 7
)

func Is(r Result) bool { return r > 1 }
func UserAgent(ua string) Result {
	if len(ua) < 10 || !strings.Contains(ua, " ") || !strings.Contains(ua, "/") {
		return BotShort
	}
	for _, name := range knownBrowsers {
		if strings.Contains(ua, name) {
			return NoBotKnown
		}
	}
	if strings.Contains(ua, "://") {
		return BotLink
	}
	for _, name := range clientLibraries {
		if strings.Contains(ua, name) {
			return BotClientLibrary
		}
	}
	for _, name := range knownBots {
		if strings.Contains(ua, name) {
			return BotKnownBot
		}
	}
	lower := strings.ToLower(ua)
	for _, word := range []string{"bot", "crawler", "spider"} {
		if strings.Contains(lower, word) {
			return BotBoty
		}
	}
	return NoBotNoMatch
}
func Bot(r *http.Request) Result {
	for _, name := range []string{"X-Moz", "X-Purpose", "Purpose", "Sec-Purpose"} {
		v := r.Header.Get(name)
		if v == "prefetch" || v == "preview" || strings.HasPrefix(v, "prefetch;") {
			return BotPrefetch
		}
	}
	result := UserAgent(r.UserAgent())
	if Is(result) {
		return result
	}
	return ipRange(r.RemoteAddr)
}

//go:embed cloud_ranges.txt
var cloudRanges string
var networks = func() map[netip.Prefix]Result {
	names := map[string]Result{"AWS": 8, "DigitalOcean": 9, "ServersCom": 10, "GoogleCloud": 11, "Hetzner": 12, "Azure": 13, "Alibaba": 14, "Linode": 15, "Oracle": 16, "OVH": 17}
	result := map[netip.Prefix]Result{}
	for _, record := range strings.Fields(cloudRanges) {
		cidr, name, _ := strings.Cut(record, ",")
		result[netip.MustParsePrefix(cidr).Masked()] = names[name]
	}
	return result
}()

func ipRange(addr string) Result {
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return NoBotKnown
	}
	ip = ip.Unmap()
	for bits := ip.BitLen(); bits >= 0; bits-- {
		if result, ok := networks[netip.PrefixFrom(ip, bits).Masked()]; ok {
			return result
		}
	}
	return NoBotNoMatch
}
