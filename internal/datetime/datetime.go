// Package datetime implements date and time utilities used by the dashboard.
package datetime

import (
	"context"
	"iter"
	"strings"
	"time"
)

type clockKey struct{}

func Now(ctx context.Context) time.Time {
	if t, ok := ctx.Value(clockKey{}).(time.Time); ok {
		return t.UTC()
	}
	return time.Now().UTC()
}
func WithNow(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, clockKey{}, t)
}

type Period int

const (
	Day Period = iota
	weekMonday
	weekSunday
	Month
	Quarter
	HalfYear
	Year
)

func Week(sunday bool) Period {
	if sunday {
		return weekSunday
	}
	return weekMonday
}
func StartOf(t time.Time, p Period) time.Time {
	y, m, d := t.Date()
	switch p {
	case weekMonday:
		d -= (int(t.Weekday()) + 6) % 7
	case weekSunday:
		d -= int(t.Weekday())
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
	case Day:
		return t.AddDate(0, 0, n)
	case weekMonday, weekSunday:
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
		return start.Format(time.DateOnly)
	}
	return start.Format(time.DateOnly) + "–" + end.Format(time.DateOnly)
}
func (r Range) Iter(p Period) iter.Seq[time.Time] {
	return func(yield func(time.Time) bool) {
		if r.Start.After(r.End) {
			return
		}
		end := EndOf(r.End, p)
		for t := StartOf(r.Start, p); !t.After(end); t = AddPeriod(t, 1, p) {
			if !yield(t) {
				return
			}
		}
	}
}

// FromString is a fixture convenience; invalid input is a programming error.
func FromString(s string) time.Time {
	layout := "2006-01-02 15:04:05"
	date := s
	zone := ""
	if i := strings.LastIndexByte(s, ' '); i > 0 && !strings.ContainsAny(s[i:], "0123456789") {
		date = s[:i]
		zone = " MST"
	}
	if len(date) < len(layout) {
		layout = layout[:len(date)]
	}
	t, err := time.Parse(layout+zone, s)
	if err != nil {
		panic(err)
	}
	return t
}
