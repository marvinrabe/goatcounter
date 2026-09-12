package cron_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zstd/ztime"
	"zgo.at/zstd/ztype"
)

func TestLanguageStats(t *testing.T) {
	ctx := testenv.DB(t)

	site := goatcounter.MustGetSite(ctx)
	site.Settings.Collect.Set(goatcounter.CollectLanguage)
	if err := site.Update(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2019, 8, 31, 14, 42, 0, 0, time.UTC)

	testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
		{CreatedAt: now, Language: ztype.Ptr("nld")},
		{CreatedAt: now, Language: ztype.Ptr("nld")},
		{CreatedAt: now, Language: ztype.Ptr("eng"), FirstVisit: true},
	}...)

	var stats goatcounter.HitStats
	err := stats.ListLanguages(ctx, ztime.NewRange(now).To(now), goatcounter.PathFilter{}, 10, 0)
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
		{CreatedAt: now, Language: ztype.Ptr("nld")},
		{CreatedAt: now, Language: ztype.Ptr("nld"), FirstVisit: true},
		{CreatedAt: now, Language: ztype.Ptr("eng"), FirstVisit: true},
		{CreatedAt: now, Language: ztype.Ptr("eng")},
		{CreatedAt: now, FirstVisit: true},
	}...)

	stats = goatcounter.HitStats{}
	err = stats.ListLanguages(ctx, ztime.NewRange(now).To(now), goatcounter.PathFilter{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}

	want = `{false [{eng English 2 <nil>} { (unknown) 1 <nil>} {nld Dutch 1 <nil>}]}`
	out = fmt.Sprintf("%v", stats)
	if want != out {
		t.Errorf("\nwant: %s\nout:  %s", want, out)
	}
}

func TestLanguageStatsNoCollect(t *testing.T) {
	ctx := testenv.DB(t)

	now := time.Date(2019, 8, 31, 14, 42, 0, 0, time.UTC)

	// CollectLanguage is off by default, so the language is dropped.
	testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
		{CreatedAt: now, Language: ztype.Ptr("nld"), FirstVisit: true},
	}...)

	var stats goatcounter.HitStats
	err := stats.ListLanguages(ctx, ztime.NewRange(now).To(now), goatcounter.PathFilter{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}

	want := `{false [{ (unknown) 1 <nil>}]}`
	out := fmt.Sprintf("%v", stats)
	if want != out {
		t.Errorf("\nwant: %s\nout:  %s", want, out)
	}
}
