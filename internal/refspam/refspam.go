// Package refspam reports whether a referrer host is a known spammer.
package refspam

import (
	"strings"
	"sync"
)

var (
	subdomains []string
	once       sync.Once
)

// Is reports whether host is a known referrer spammer, either as an exact
// match or as a subdomain of one.
func Is(host string) bool {
	if _, ok := list[host]; ok {
		return true
	}

	once.Do(func() {
		subdomains = make([]string, 0, len(list))
		for v := range list {
			subdomains = append(subdomains, "."+v)
		}
	})

	for _, v := range subdomains {
		if strings.HasSuffix(host, v) {
			return true
		}
	}
	return false
}
