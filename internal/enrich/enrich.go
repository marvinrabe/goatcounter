// Package enrich turns a pageview request into the dimensions stored about it,
// and filters out what isn't counted. There is a file for every lookup:
//
//	bot.go        IsBot: bots and prefetches aren't counted.
//	refspam.go    Referrer spam is stored as a direct visit.
//	source.go     Source: source and referrer, from the Referer.
//	useragent.go  ParseUserAgent: browser and OS, from the User-Agent.
//	location.go   Geo: country, from a CDN header or a GeoIP database.
//	language.go   Language: from the Accept-Language.
//	device.go     Device category, from the screen width; at query time.
//
// Data files are named after the lookup they're for: bot_ranges.txt,
// refspam_list.go, and so on.
//
// The bot networks and spam list change upstream. They start from the snapshot
// compiled in, and Datasets keeps them up to date.
package enrich

import "github.com/marvinrabe/goatcounter/internal/dataset"

// Datasets that have an upstream to update from, for dataset.Run.
func Datasets() []dataset.Refresher {
	sets := []dataset.Refresher{spammers}
	for _, d := range cloudRanges {
		if d.Updatable() {
			sets = append(sets, d)
		}
	}
	return sets
}
