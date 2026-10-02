package analytics

type Group uint8

func (g Group) Hourly() bool  { return g == GroupHourly }
func (g Group) Weekly() bool  { return g == GroupWeekly }
func (g Group) Monthly() bool { return g == GroupMonthly }
func (g Group) Yearly() bool  { return g == GroupYearly }
func (g Group) String() string {
	switch g {
	case GroupDaily:
		return "day"
	case GroupWeekly:
		return "week"
	case GroupMonthly:
		return "month"
	case GroupYearly:
		return "year"
	}
	return "hour"
}

// Groups are the groupings the dashboard offers for a period.
type Groups []Group

const (
	GroupHourly = Group(iota)
	GroupDaily
	GroupWeekly
	GroupMonthly
	GroupYearly
)

type HitList struct {
	// Number of visitors for the selected date range.
	Count int `db:"count" json:"count"`

	// Path name (e.g. /hello.html).
	Path string `db:"path" json:"path"`
}

type HitLists []HitList
