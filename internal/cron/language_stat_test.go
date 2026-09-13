package cron_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func TestLanguageStats(t *testing.T) {
	ctx := testenv.DB(t)

	now := time.Date(2019, 8, 31, 14, 42, 0, 0, time.UTC)

	testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
		{CreatedAt: now, Language: new("nld")},
		{CreatedAt: now, Language: new("nld")},
		{CreatedAt: now, Language: new("eng"), FirstVisit: true},
	}...)

	var stats goatcounter.HitStats
	err := stats.ListLanguages(ctx, datetime.NewRange(now).To(now), goatcounter.PathFilter{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}

	want := `{false [{eng English 1 <nil>}]}`
	out := fmt.Sprintf("%v", stats)
	if want != out {
		t.Errorf("\nwant: %s\nout:  %s", want, out)
	}

	// Update existing, and store a hit without a language.
	testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
		{CreatedAt: now, Language: new("nld")},
		{CreatedAt: now, Language: new("nld"), FirstVisit: true},
		{CreatedAt: now, Language: new("eng"), FirstVisit: true},
		{CreatedAt: now, Language: new("eng")},
		{CreatedAt: now, FirstVisit: true},
	}...)

	stats = goatcounter.HitStats{}
	err = stats.ListLanguages(ctx, datetime.NewRange(now).To(now), goatcounter.PathFilter{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}

	want = `{false [{eng English 2 <nil>} { (unknown) 1 <nil>} {nld Dutch 1 <nil>}]}`
	out = fmt.Sprintf("%v", stats)
	if want != out {
		t.Errorf("\nwant: %s\nout:  %s", want, out)
	}
}

func TestLanguageStatsAlwaysCollected(t *testing.T) {
	ctx := testenv.DB(t)

	now := time.Date(2019, 8, 31, 14, 42, 0, 0, time.UTC)

	// Language is collected without enabling a site setting.
	testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
		{CreatedAt: now, Language: new("nld"), FirstVisit: true},
	}...)

	var stats goatcounter.HitStats
	err := stats.ListLanguages(ctx, datetime.NewRange(now).To(now), goatcounter.PathFilter{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}

	want := `{false [{nld Dutch 1 <nil>}]}`
	out := fmt.Sprintf("%v", stats)
	if want != out {
		t.Errorf("\nwant: %s\nout:  %s", want, out)
	}
}
