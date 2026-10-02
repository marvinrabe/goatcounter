package analytics

import (
	"strings"
	"testing"
	"time"
)

func TestCalendarAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	for _, start := range []time.Time{time.Date(2026, 3, 28, 0, 0, 0, 0, loc), time.Date(2026, 10, 24, 0, 0, 0, 0, loc)} {
		for n := range 3 {
			day := AddPeriod(StartOf(start, Day), n, Day)
			if day.Hour() != 0 || day.Day() != start.Day()+n {
				t.Errorf("unexpected day: %s", day)
			}
			if end := EndOf(day, Day); end.Hour() != 23 || end.Day() != day.Day() {
				t.Errorf("unexpected end of day: %s", end)
			}
		}
	}
}
func TestMonthEndClamping(t *testing.T) {
	for _, tt := range []struct {
		date, want string
		n          int
	}{
		{"2024-01-31", "2024-02-29", 1}, {"2025-03-31", "2025-02-28", -1}, {"2024-02-29", "2025-02-28", 12},
	} {
		if got := AddPeriod(fromString(tt.date), tt.n, Month).Format(time.DateOnly); got != tt.want {
			t.Errorf("%s + %d months = %s; want %s", tt.date, tt.n, got, tt.want)
		}
	}
}

func TestRangeString(t *testing.T) {
	for _, tt := range []struct {
		start, end, want string
	}{
		{"2026-08-02", "2026-09-02", "2 Aug – 2 Sep 2026"},
		{"2026-08-02", "2026-08-09", "2 Aug – 9 Aug 2026"},
		{"2025-12-30", "2026-01-02", "30 Dec 2025 – 2 Jan 2026"},
		{"2026-08-02", "2026-08-02", "2 Aug 2026"},
	} {
		rng := NewRange(fromString(tt.start)).To(fromString(tt.end))
		if got := rng.String(); got != tt.want {
			t.Errorf("%s to %s: got %q, want %q", tt.start, tt.end, got, tt.want)
		}
	}
}

// fromString parses a fixture date; invalid input is a programming error.
func fromString(s string) time.Time {
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
