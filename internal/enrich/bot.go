package enrich

import (
	_ "embed"
	"io"
	"net/http"
	"net/netip"
	"strings"

	"github.com/marvinrabe/goatcounter/internal/dataset"
)

// IsBot reports whether the request is from a bot or a prefetch, which aren't
// counted. Bots are recognized by their User-Agent (bot_signatures.go), and by
// coming from a hosting provider's network (cloudRanges), as people don't
// browse from servers.
//
// The signatures and the network snapshot are from isbot v1.1.0, under the MIT
// license in LICENSE.isbot.
func IsBot(r *http.Request) bool {
	for _, name := range []string{"X-Moz", "X-Purpose", "Purpose", "Sec-Purpose"} {
		v := r.Header.Get(name)
		if v == "prefetch" || v == "preview" || strings.HasPrefix(v, "prefetch;") {
			return true
		}
	}
	if botUserAgent(r.UserAgent()) {
		return true
	}
	ip, err := netip.ParseAddr(r.RemoteAddr)
	if err != nil {
		return false
	}
	for _, p := range cloudRanges {
		if p.Load().Contains(ip) {
			return true
		}
	}
	return false
}

func botUserAgent(ua string) bool {
	if len(ua) < 10 || !strings.Contains(ua, " ") || !strings.Contains(ua, "/") {
		return true
	}
	for _, name := range knownBrowsers {
		if strings.Contains(ua, name) {
			return false
		}
	}
	if strings.Contains(ua, "://") {
		return true
	}
	for _, list := range [][]string{clientLibraries, knownBots} {
		for _, name := range list {
			if strings.Contains(ua, name) {
				return true
			}
		}
	}
	lower := strings.ToLower(ua)
	for _, word := range []string{"bot", "crawler", "spider"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// cloudRanges has a dataset of networks for every hosting provider. They start
// from the snapshot in bot_ranges.txt; the providers that publish their
// networks are kept up to date from there.
var cloudRanges = func() []*dataset.Dataset[Ranges] {
	snapshot := map[string][]netip.Prefix{}
	for _, record := range strings.Fields(cloudSnapshot) {
		cidr, name, _ := strings.Cut(record, ",")
		snapshot[name] = append(snapshot[name], netip.MustParsePrefix(cidr))
	}
	p := func(name, url string, parse func(io.Reader) (Ranges, error)) *dataset.Dataset[Ranges] {
		return dataset.New("cloud ranges "+name, NewRanges(snapshot[name]), url, parse)
	}
	return []*dataset.Dataset[Ranges]{
		p("AWS", "https://ip-ranges.amazonaws.com/ip-ranges.json", parseAWS),
		p("DigitalOcean", "https://digitalocean.com/geo/google.csv", parseGeofeed),
		p("GoogleCloud", "https://www.gstatic.com/ipranges/cloud.json", parseGoogleCloud),
		p("Linode", "https://geoip.linode.com/", parseGeofeed),
		p("Oracle", "https://docs.oracle.com/en-us/iaas/tools/public_ip_ranges.json", parseOracle),
		// No stable upstream; Azure's download URL changes every week.
		p("Azure", "", nil),
		p("Hetzner", "", nil),
		p("Alibaba", "", nil),
		p("OVH", "", nil),
	}
}()

//go:embed bot_ranges.txt
var cloudSnapshot string
