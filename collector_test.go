package goatcounter_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/cron"
	"github.com/marvinrabe/goatcounter/internal/database"
	libsqldriver "github.com/marvinrabe/goatcounter/internal/dbdriver/libsql"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func queryCount(t *testing.T, ctx context.Context, query string) int {
	t.Helper()
	var n int
	if err := database.Get(ctx, &n, query); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDurableCollectorRollback(t *testing.T) {
	for _, target := range []string{"hits", "size_stats"} {
		t.Run(target, func(t *testing.T) {
			ctx := testenv.DB(t)
			h := Hit{Path: "/retry", UserAgentHeader: "browser", RemoteAddr: "192.0.2.1"}
			if err := EnqueueHits(ctx, h); err != nil {
				t.Fatal(err)
			}
			if n := queryCount(t, ctx, "select count(*) from hit_queue"); n != 1 {
				t.Fatalf("durable queue: %d", n)
			}
			if err := database.Exec(ctx, `create trigger reject_write before insert on `+target+`
                begin select raise(abort, 'injected failure'); end`); err != nil {
				t.Fatal(err)
			}
			if _, err := PersistHits(ctx, cron.UpdateStats); err == nil {
				t.Fatal("expected write failure")
			}
			for _, table := range []string{"hits", "collector_sessions", "collector_session_paths", "hit_counts", "paths"} {
				if n := queryCount(t, ctx, "select count(*) from "+table); n != 0 {
					t.Errorf("%s: partial commit of %d rows", table, n)
				}
			}
			if n := queryCount(t, ctx, "select count(*) from hit_queue"); n != 1 {
				t.Fatalf("lost queued hit: %d", n)
			}
			if err := database.Exec(ctx, `drop trigger reject_write`); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, err := PersistHits(ctx, cron.UpdateStats); err != nil {
					t.Fatal(err)
				}
			}
			for query, want := range map[string]int{"select count(*) from hits": 1, "select sum(total) from hit_counts": 1, "select count(*) from hit_queue": 0} {
				if got := queryCount(t, ctx, query); got != want {
					t.Errorf("%s: %d, want %d", query, got, want)
				}
			}
		})
	}
}

func TestCollectorsShareSessionAndQueue(t *testing.T) {
	first := testenv.DB(t)
	db, err := libsqldriver.Open(context.Background(), database.ConnectOptions{Connect: os.Getenv("TESTENV_CONNECT"), Create: false})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	second := testenv.Context(db)
	h := Hit{Path: "/same", UserAgentHeader: "browser", RemoteAddr: "192.0.2.1"}
	for i := 0; i < 40; i++ {
		if err := EnqueueHits(first, h); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, c := range []context.Context{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(c, 10*time.Second)
			defer cancel()
			if _, err := PersistHits(c, cron.UpdateStats); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	// A fresh context has no local visitor state, just like a replacement pod.
	replacement := testenv.Context(db)
	if err := EnqueueHits(replacement, h); err != nil {
		t.Fatal(err)
	}
	if _, err := PersistHits(replacement, cron.UpdateStats); err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]int{"select count(*) from hits": 41, "select count(distinct session) from hits": 1,
		"select sum(first_visit) from hits": 1, "select sum(total) from hit_counts": 1, "select count(*) from hit_queue": 0} {
		if got := queryCount(t, first, query); got != want {
			t.Errorf("%s: %d, want %d", query, got, want)
		}
	}
}

func TestCollectorBatchBounded(t *testing.T) {
	ctx := testenv.DB(t)
	hits := make([]Hit, HitBatchSize+5)
	for i := range hits {
		hits[i] = Hit{Path: fmt.Sprintf("/page-%d", i), Session: TestSession, FirstVisit: true}
	}
	if err := EnqueueHits(ctx, hits...); err != nil {
		t.Fatal(err)
	}
	batch, err := PersistHits(ctx, cron.UpdateStats)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != HitBatchSize {
		t.Fatalf("batch length: %d", len(batch))
	}
	if n := queryCount(t, ctx, "select count(*) from hit_queue"); n != 5 {
		t.Fatalf("remaining queue: %d", n)
	}
}
