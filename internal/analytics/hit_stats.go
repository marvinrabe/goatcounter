package analytics

type HitStat struct {
	// ID for selecting more details; not present in the detail view.
	ID    string `db:"id" json:"id,omitempty"`
	Name  string `db:"name" json:"name"`   // Display name.
	Count int    `db:"count" json:"count"` // Number of visits.

	// What kind of referral this is; only set when retrieving referrals {enum: h g}.
	//
	//  h   A domain or URL, which can be linked.
	//  g   Generated; for example "Google" or a utm_source value.
	RefScheme *string `db:"-" json:"ref_scheme,omitempty"`
}

type HitStats struct {
	More  bool      `json:"more"`
	Stats []HitStat `json:"stats"`
}

const (
	SizePhones  = "phone"
	SizeTablets = "tablet"
	SizeDesktop = "desktop"
	SizeUnknown = "unknown"
)

// HitStat.RefScheme values.
const (
	RefSchemeHTTP      = "h"
	RefSchemeGenerated = "g"
)
