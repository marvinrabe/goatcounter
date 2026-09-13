package cron_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func TestDataRetentionForever(t *testing.T) {
	ctx := testenv.DB(t)

	now := time.Now().UTC()
	past := now.Add(-40 * 24 * time.Hour)

	testenv.StoreHits(ctx, t, false, []goatcounter.Hit{
		{CreatedAt: now, Path: "/a", FirstVisit: true},
		{CreatedAt: now, Path: "/a", FirstVisit: false},
		{CreatedAt: past, Path: "/a", FirstVisit: true},
		{CreatedAt: past, Path: "/a", FirstVisit: false},
	}...)

	var hits goatcounter.Hits
	err := hits.TestList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 4 {
		t.Errorf("len(hits) is %d\n%v", len(hits), hits)
	}

	var stats goatcounter.HitLists
	display, more, err := stats.List(ctx,
		datetime.NewRange(past.Add(-1*24*time.Hour)).To(now),
		goatcounter.PathFilter{}, nil, 10, goatcounter.GroupHourly)
	if err != nil {
		t.Fatal(err)
	}

	out := fmt.Sprintf("%d %t %v", display, more, err)
	want := `2 false <nil>`
	if out != want {
		t.Errorf("\ngot:  %s\nwant: %s", out, want)
	}
}
