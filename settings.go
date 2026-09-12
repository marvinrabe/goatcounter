package goatcounter

import (
	"context"
	"zgo.at/zstd/zint"
)

const (
	CollectNothing zint.Bitflag16 = 1 << iota
	CollectReferrer
	CollectUserAgent
	CollectScreenSize
	CollectLocation
	CollectLocationRegion
	CollectLanguage
	CollectSession
	CollectHits
)

const CollectAll = CollectReferrer | CollectUserAgent | CollectScreenSize | CollectLocation |
	CollectLocationRegion | CollectLanguage | CollectSession | CollectHits

// SiteSettings remains as an internal compatibility surface. These values are
// deliberately not configurable: dashboards are private, data is retained
// forever, and every supported data point is collected.
type SiteSettings struct {
	Public         string
	DataRetention  int
	Collect        zint.Bitflag16
	CollectRegions Strings
}

func (ss *SiteSettings) Defaults(context.Context) {
	ss.Public = "private"
	ss.DataRetention = 0
	ss.Collect = CollectAll
	ss.CollectRegions = nil
}
func (ss SiteSettings) CanView(string) bool { return false }
func (ss SiteSettings) IsPublic() bool      { return false }
