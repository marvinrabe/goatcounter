package goatcounter_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func TestBatchCacheTransactionIsolation(t *testing.T) {
	ctx := testenv.DB(t)
	ctx, queries := testenv.CountInlineQueries(ctx)
	abort := errors.New("rollback")
	var previous context.Context
	err := database.TX(ctx, func(txctx context.Context) error {
		txctx = NewBatchCache(txctx)
		previous = txctx
		for range 2 {
			if err := (&Path{Path: "/rollback"}).GetOrInsert(txctx); err != nil {
				return err
			}
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	if n := queries.Count("Path.GetOrInsert"); n != 1 {
		t.Fatalf("batch path reads = %d; want 1", n)
	}
	// Reusing context values with a different transaction must not reuse its IDs.
	ctx = database.WithDB(previous, database.MustGetDB(ctx))
	err = database.TX(ctx, func(txctx context.Context) error {
		return (&Path{Path: "/rollback"}).GetOrInsert(txctx)
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := queries.Count("Path.GetOrInsert"); n != 2 {
		t.Fatalf("next transaction reused cached path: %d reads", n)
	}
	var p Path
	if err := p.ByPath(ctx, "/rollback"); err != nil {
		t.Fatalf("path never committed: %v", err)
	}
}

func TestLocationCacheCopiesValues(t *testing.T) {
	ctx := testenv.DB(t)
	if err := database.Exec(ctx, `insert into locations(country, region, country_name, region_name) values ('IE', '', 'Ireland', ''), ('US', '', 'United States', '')`); err != nil {
		t.Fatal(err)
	}
	ctx, queries := testenv.CountInlineQueries(ctx)
	var loc Location
	for _, code := range []string{"IE", "US", "IE", "US"} {
		if err := loc.ByCode(ctx, code); err != nil {
			t.Fatal(err)
		}
		if loc.Country != code {
			t.Fatalf("%s returned %#v", code, loc)
		}
		loc.Country = "mutated receiver"
	}
	if n := queries.Count("locations where iso_3166_2"); n != 2 {
		t.Fatalf("location reads = %d; want 2", n)
	}
}

func TestLocationCacheRollback(t *testing.T) {
	ctx := testenv.DB(t)
	abort := errors.New("rollback")
	for _, batch := range []bool{false, true} {
		err := database.TX(ctx, func(txctx context.Context) error {
			if batch {
				txctx = NewBatchCache(txctx)
			}
			if err := (&Location{}).Lookup(txctx, "51.171.91.33"); err != nil {
				return err
			}
			return abort
		})
		if !errors.Is(err, abort) {
			t.Fatal(err)
		}
	}
	if err := (&Location{}).Lookup(ctx, "51.171.91.33"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.Get(ctx, &count, `select count(*) from locations where country='IE'`); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("uncommitted location escaped into the shared cache")
	}
}
