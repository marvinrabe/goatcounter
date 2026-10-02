package analytics

import "time"

// Period is a span of calendar time. It's used both to select a dashboard
// range ("last month") and to group the chart (per day, week, …).
type Period uint8

// Weeks start on Monday.
const (
	Hour Period = iota
	Day
	Week
	Month
	Quarter
	HalfYear
	Year
)

// ChartGroups are the periods the dashboard chart can be grouped by.
var ChartGroups = []Period{Hour, Day, Week, Month, Year}

func (p Period) String() string {
	switch p {
	case Hour:
		return "hour"
	case Day:
		return "day"
	case Week:
		return "week"
	case Month:
		return "month"
	case Quarter:
		return "quarter"
	case HalfYear:
		return "half-year"
	}
	return "year"
}

func StartOf(t time.Time, p Period) time.Time {
	if p == Hour {
		return t.Truncate(time.Hour)
	}
	y, m, d := t.Date()
	switch p {
	case Week:
		d -= (int(t.Weekday()) + 6) % 7
	case Month:
		d = 1
	case Quarter:
		m = time.Month((int(m)-1)/3*3 + 1)
		d = 1
	case HalfYear:
		m = time.Month((int(m)-1)/6*6 + 1)
		d = 1
	case Year:
		m = 1
		d = 1
	}
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
func EndOf(t time.Time, p Period) time.Time { return AddPeriod(StartOf(t, p), 1, p).Add(-time.Second) }
func AddPeriod(t time.Time, n int, p Period) time.Time {
	switch p {
	case Hour:
		return t.Add(time.Duration(n) * time.Hour)
	case Day:
		return t.AddDate(0, 0, n)
	case Week:
		return t.AddDate(0, 0, n*7)
	}
	months := n
	switch p {
	case Quarter:
		months *= 3
	case HalfYear:
		months *= 6
	case Year:
		months *= 12
	}
	// Clamp to the final day instead of rolling January 31 into March.
	first := time.Date(t.Year(), t.Month()+time.Month(months), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
	return first.AddDate(0, 0, min(t.Day(), last)-1)
}

type Range struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func NewRange(t time.Time) Range       { return Range{Start: t} }
func (r Range) To(t time.Time) Range   { r.End = t.In(r.Start.Location()); return r }
func (r Range) From(t time.Time) Range { r.Start = t; r.End = r.End.In(t.Location()); return r }
func (r Range) In(loc *time.Location) Range {
	r.Start = r.Start.In(loc)
	r.End = r.End.In(loc)
	return r
}
func (r Range) UTC() Range                { return r.In(time.UTC) }
func (r Range) Add(d time.Duration) Range { r.Start = r.Start.Add(d); r.End = r.End.Add(d); return r }
func (r Range) Current(p Period) Range    { return Range{StartOf(r.Start, p), EndOf(r.Start, p)} }
func (r Range) Last(p Period) Range {
	return Range{StartOf(AddPeriod(r.Start, -1, p), Day), EndOf(r.Start, Day)}
}
func (r Range) String() string {
	start, end := StartOf(r.Start, Day), StartOf(r.End, Day)
	today := StartOf(time.Now().In(start.Location()), Day)
	if start.Equal(end) {
		if start.Equal(today) {
			return "Today"
		}
		if start.Equal(today.AddDate(0, 0, -1)) {
			return "Yesterday"
		}
		return start.Format("2 Jan 2006")
	}
	if start.Year() == end.Year() {
		return start.Format("2 Jan") + " – " + end.Format("2 Jan 2006")
	}
	return start.Format("2 Jan 2006") + " – " + end.Format("2 Jan 2006")
}
