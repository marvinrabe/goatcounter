package cron_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func TestLocationStats(t *testing.T) {
	ctx := testenv.DB(t)

	now := time.Date(2019, 8, 31, 14, 42, 0, 0, time.UTC)

	testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
		{CreatedAt: now, Location: "ID"},
		{CreatedAt: now, Location: "ID"},
		{CreatedAt: now, Location: "ET", FirstVisit: true},
	}...)

	var stats goatcounter.HitStats
	err := stats.ListLocations(ctx, datetime.NewRange(now).To(now), goatcounter.PathFilter{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}

	want := `{false [{ET Ethiopia 1 <nil>}]}`
	out := fmt.Sprintf("%v", stats)
	if want != out {
		t.Errorf("\nwant: %s\nout:  %s", want, out)
	}

	// Update existing.
	testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
		{CreatedAt: now, Location: "ID"},
		{CreatedAt: now, Location: "ID", FirstVisit: true},
		{CreatedAt: now, Location: "ET"},
		{CreatedAt: now, Location: "ET", FirstVisit: true},
		{CreatedAt: now, Location: "ET", FirstVisit: true},
		{CreatedAt: now, Location: "ET"},
		{CreatedAt: now, Location: "NZ", FirstVisit: true},
	}...)

	stats = goatcounter.HitStats{}
	err = stats.ListLocations(ctx, datetime.NewRange(now).To(now), goatcounter.PathFilter{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}

	want = `{false [{ET Ethiopia 3 <nil>} {ID Indonesia 1 <nil>} {NZ New Zealand 1 <nil>}]}`
	out = fmt.Sprintf("%v", stats)
	if want != out {
		t.Errorf("\nwant: %s\nout:  %s", want, out)
	}
}
