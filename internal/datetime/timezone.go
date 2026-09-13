package datetime

import (
	"fmt"
	"os"
	"time"
)

// Timezone is the timezone the dashboard is displayed in.
//
// There is just one timezone for the entire installation, set with the TZ
// environment variable; the zero value is UTC.
type Timezone struct{ loc *time.Location }

// LoadTimezone gets the timezone from the TZ environment variable, falling back
// to the system timezone (which Go also derives from TZ) if it's not set.
func LoadTimezone() (Timezone, error) {
	name := os.Getenv("TZ")
	if name == "" {
		return Timezone{time.Local}, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return Timezone{time.UTC}, fmt.Errorf("TZ=%q: %w", name, err)
	}
	return Timezone{loc}, nil
}

// Loc gets the time.Location, or UTC if it's not set.
func (t Timezone) Loc() *time.Location {
	if t.loc == nil {
		return time.UTC
	}
	return t.loc
}

// String is the zone name, e.g. "Europe/Berlin".
func (t Timezone) String() string { return t.Loc().String() }

// Abbr is the abbreviation currently in effect, e.g. "CEST".
func (t Timezone) Abbr() string {
	n, _ := time.Now().In(t.Loc()).Zone()
	return n
}

// Offset from UTC currently in effect, in minutes.
func (t Timezone) Offset() int {
	_, o := time.Now().In(t.Loc()).Zone()
	return o / 60
}

// OffsetDuration is Offset() as a time.Duration.
func (t Timezone) OffsetDuration() time.Duration {
	return time.Duration(t.Offset()) * time.Minute
}

// OffsetDisplay formats the offset for display, e.g. "+02:00".
func (t Timezone) OffsetDisplay() string {
	o, sign := t.Offset(), "+"
	if o < 0 {
		o, sign = -o, "-"
	}
	return fmt.Sprintf("%s%02d:%02d", sign, o/60, o%60)
}
