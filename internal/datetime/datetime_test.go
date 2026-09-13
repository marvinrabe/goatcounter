package datetime

import (
	"testing"
	"time"
)

func TestCalendarAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	for _, start := range []time.Time{time.Date(2026, 3, 28, 0, 0, 0, 0, loc), time.Date(2026, 10, 24, 0, 0, 0, 0, loc)} {
		rng := NewRange(start).To(EndOf(start.AddDate(0, 0, 2), Day))
		n := 0
		for day := range rng.Iter(Day) {
			if day.Hour() != 0 || day.Day() != start.Day()+n {
				t.Errorf("unexpected day: %s", day)
			}
			n++
		}
		if n != 3 {
			t.Errorf("iterated %d days", n)
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
		if got := AddPeriod(FromString(tt.date), tt.n, Month).Format(time.DateOnly); got != tt.want {
			t.Errorf("%s + %d months = %s; want %s", tt.date, tt.n, got, tt.want)
		}
	}
}
