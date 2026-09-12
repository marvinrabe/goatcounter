package goatcounter

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"
	"unicode"

	"github.com/marvinrabe/goatcounter/internal/i18n"
	"zgo.at/json"
	"zgo.at/zstd/zint"
	"zgo.at/zstd/zjson"
	"zgo.at/zvalidate"
)

// Settings.Collect values (bitmask)
//
// DO NOT change the values of these constants; they're stored in the database.
//
// Note: also update CollectFlags() method below.
//
// Nothing is 1 and 0 is "unset". This is so we can distinguish between "this
// field was never sent in the form" vs. "user unchecked all boxes".
const (
	CollectNothing        zint.Bitflag16 = 1 << iota
	CollectReferrer                      // 2
	CollectUserAgent                     // 4
	CollectScreenSize                    // 8
	CollectLocation                      // 16
	CollectLocationRegion                // 32
	CollectLanguage                      // 64
	CollectSession                       // 128
	CollectHits                          // 256
)

type (
	// SiteSettings contains all the configurable settings for this
	// installation, stored as JSON in the database.
	SiteSettings struct {
		Public         string         `json:"public"`
		Secret         string         `json:"secret"`
		DataRetention  int            `json:"data_retention"`
		Campaigns      Strings        `json:"-"`
		IgnoreIPs      Strings        `json:"ignore_ips"`
		Collect        zint.Bitflag16 `json:"collect"`
		CollectRegions Strings        `json:"collect_regions"`

		// Don't show exact numbers on the dashboard.
		FewerNumbers          bool      `json:"fewer_numbers"`
		FewerNumbersLockUntil time.Time `json:"fewer_numbers_lock_until"`
	}
)

func (ss SiteSettings) String() string               { return string(zjson.MustMarshal(ss)) }
func (ss SiteSettings) Value() (driver.Value, error) { return json.Marshal(ss) }
func (ss *SiteSettings) Scan(v any) error {
	switch vv := v.(type) {
	case []byte:
		return json.Unmarshal(vv, ss)
	case string:
		return json.Unmarshal([]byte(vv), ss)
	default:
		return fmt.Errorf("SiteSettings.Scan: unsupported type: %T", v)
	}
}
func (ss *SiteSettings) Defaults(ctx context.Context) {
	if ss.Public == "" {
		ss.Public = "private"
	}
	if ss.Collect == 0 {
		ss.Collect = CollectReferrer | CollectUserAgent | CollectScreenSize | CollectLocation | CollectLocationRegion | CollectSession
	}
	if ss.Collect.Has(CollectLocationRegion) { // Collecting region without country makes no sense.
		ss.Collect |= CollectLocation
	}
	if ss.CollectRegions == nil {
		ss.CollectRegions = []string{"US", "RU", "CN"}
	}
}

func (ss *SiteSettings) Validate(ctx context.Context) error {
	v := NewValidate(ctx)

	v.Include("public", ss.Public, []string{"private", "secret", "public"})
	if ss.Public == "secret" {
		v.Len("secret", ss.Secret, 8, 40)
		v.Contains("secret", ss.Secret, []*unicode.RangeTable{zvalidate.AlphaNumeric}, nil)
	}

	if ss.DataRetention > 0 {
		v.Range("data_retention", int64(ss.DataRetention), 31, 365*5)
	}

	if len(ss.IgnoreIPs) > 0 {
		for _, ip := range ss.IgnoreIPs {
			v.IP("ignore_ips", ip)
		}
	}

	return v.ErrorOrNil()
}

func (ss SiteSettings) CanView(token string) bool {
	return ss.Public == "public" || (ss.Public == "secret" && token == ss.Secret)
}

func (ss SiteSettings) IsPublic() bool {
	return ss.Public == "public"
}

type CollectFlag struct {
	Label, Help string
	Flag        zint.Bitflag16
}

// CollectFlags returns a list of all flags we know for the Collect settings.
func (ss SiteSettings) CollectFlags(ctx context.Context) []CollectFlag {
	return []CollectFlag{
		{
			Label: i18n.T(ctx, "data-collect/label/hits|Individual pageviews"),
			Help:  i18n.T(ctx, "data-collect/help/hits|Store individual pageviews for exports. This doesn’t affect anything else. The API can still be used to export aggregate data."),
			Flag:  CollectHits,
		},
		{
			Label: i18n.T(ctx, "data-collect/label/sessions|Sessions"),
			Help:  i18n.T(ctx, "data-collect/help/sessions|%[Track unique visitors] for up to 8 hours; if you disable this then someone pressing e.g. F5 to reload the page will just show as 2 pageviews instead of 1.", i18n.Tag("a", fmt.Sprintf(`href="%s/help/sessions"`, Config(ctx).BasePath))),
			Flag:  CollectSession,
		},
		{
			Label: i18n.T(ctx, "data-collect/label/referrer|Referrer"),
			Help:  i18n.T(ctx, "data-collect/help/referrer|Referer header and campaign parameters."),
			Flag:  CollectReferrer,
		},
		{
			Label: i18n.T(ctx, "data-collect/label/user-agent|User-Agent"),
			Help:  i18n.T(ctx, "data-collect/help/user-agent|Browser and system name derived from the User-Agent header (the header itself is not stored)."),
			Flag:  CollectUserAgent,
		},
		{
			Label: i18n.T(ctx, "data-collect/label/size|Size"),
			Help:  i18n.T(ctx, "data-collect/help/size|Screen size."),
			Flag:  CollectScreenSize,
		},
		{
			Label: i18n.T(ctx, "data-collect/label/country|Country"),
			Help:  i18n.T(ctx, "data-collect/help/country|Country name, for example Belgium, Indonesia, etc."),
			Flag:  CollectLocation,
		},
		{
			Label: i18n.T(ctx, "data-collect/label/region|Region"),
			Help:  i18n.T(ctx, "data-collect/help/region|Region, for example Texas, Bali, etc. The details for this differ per country."),
			Flag:  CollectLocationRegion,
		},
		{
			Label: i18n.T(ctx, "data-collect/label/language|Language"),
			Help:  i18n.T(ctx, "data-collect/help/language|Supported languages from Accept-Language."),
			Flag:  CollectLanguage,
		},
	}
}
