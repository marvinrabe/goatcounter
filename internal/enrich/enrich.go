// Package enrich turns a pageview request into the dimensions stored about it,
// and filters out what isn't counted. There is a file for every lookup:
//
//	bot.go        IsBot: bots and prefetches aren't counted.
//	refspam.go    Referrer spam is stored as a direct visit.
//	source.go     Source: source and referrer, from the Referer.
//	useragent.go  ParseUserAgent: browser, OS, and device, from the User-Agent.
//	location.go   Country: from the CDN's header.
//	language.go   Language: from the Accept-Language.
//	dataset.go    Downloads the upstream lists in the background.
//	cdn.go        Request headers set by the CDN in front.
//
// Data files are named after the lookup they're for: bot_ranges.txt,
// refspam_list.go, and so on.
//
// The bot networks and spam list change upstream. They start from the snapshot
// compiled in, and Update keeps them up to date.
package enrich

import "context"

// Update keeps the datasets that have an upstream up to date until ctx is
// cancelled.
func Update(ctx context.Context) {
	sets := []refresher{spammers}
	for _, d := range cloudRanges {
		if d.updatable() {
			sets = append(sets, d)
		}
	}
	run(ctx, sets...)
}
