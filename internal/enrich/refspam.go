package enrich

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// spammers is Matomo's referrer-spam-list. It starts from the snapshot in
// refspam_list.go, which "go generate" updates.
var spammers = newDataset("referrer spam", spamSnapshot,
	"https://raw.githubusercontent.com/matomo-org/referrer-spam-list/master/spammers.txt",
	parseSpammers)

// isSpam reports whether host is a known referrer spammer, either as an exact
// match or as a subdomain of one.
func isSpam(host string) bool {
	list := spammers.Load()
	for {
		if _, ok := list[host]; ok {
			return true
		}
		_, parent, ok := strings.Cut(host, ".")
		if !ok {
			return false
		}
		host = parent
	}
}

// parseSpammers reads spammers.txt: one host per line.
func parseSpammers(r io.Reader) (map[string]struct{}, error) {
	hosts := map[string]struct{}{"localhost": {}}
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		if h := strings.ToLower(strings.TrimSpace(scan.Text())); h != "" && h[0] != '#' {
			hosts[h] = struct{}{}
		}
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	// The list has a few thousand entries; much less is a broken download.
	if len(hosts) < 1000 {
		return nil, fmt.Errorf("only %d hosts; expected at least 1000", len(hosts))
	}
	return hosts, nil
}
