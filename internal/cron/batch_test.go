package cron_test

import (
	"context"
	"testing"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/cron"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zdb"
)

func TestUpdateStatsSkipsEmptyBatches(t *testing.T) {
	// No database or configured site: batches without counts need neither.
	for _, hits := range [][]goatcounter.Hit{nil, {{FirstVisit: false}}, {{Bot: 1, FirstVisit: true}}} {
		if err := cron.UpdateStats(context.Background(), hits); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUpdateStatsAtomicBatch(t *testing.T) {
	ctx := testenv.DB(t)
	hits := testenv.StoreHits(ctx, t, false, goatcounter.Hit{Path: "/one", FirstVisit: true})
	if err := zdb.Exec(ctx, `create trigger reject_sizes before insert on size_stats
		begin select raise(abort, 'reject size batch'); end`); err != nil {
		t.Fatal(err)
	}
	if err := cron.UpdateStats(ctx, hits); err == nil {
		t.Fatal("expected aggregate write failure")
	}

	for _, table := range []string{"hit_counts", "ref_counts", "location_stats", "language_stats", "size_stats"} {
		column := "count"
		if table == "hit_counts" || table == "ref_counts" {
			column = "total"
		}
		var count int
		if err := zdb.Get(ctx, &count, "select sum("+column+") from "+table); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("%s was partially updated: %d", table, count)
		}
	}
	if err := zdb.Exec(ctx, `drop trigger reject_sizes`); err != nil {
		t.Fatal(err)
	}
	if err := cron.UpdateStats(ctx, hits); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := zdb.Get(ctx, &count, `select sum(total) from hit_counts`); err != nil || count != 2 {
		t.Fatalf("retry: got %d, %v", count, err)
	}
}
