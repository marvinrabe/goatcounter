package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/marvinrabe/goatcounter/internal/database"
)

func testDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Exec(context.Background(), `create table items(id integer primary key, name text)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestBulkInsertRejectsNames(t *testing.T) {
	db, ctx := testDB(t), context.Background()
	for _, cols := range [][]string{{`name"; drop table items; --`}, {"Name"}, {"1id"}, {""}} {
		if _, err := newBulkInsert(ctx, db, "items", cols); err == nil {
			t.Errorf("%q accepted", cols)
		}
	}
	if _, err := newBulkInsert(ctx, db, "items; --", []string{"id"}); err == nil {
		t.Error("table name accepted")
	}
	if _, err := newBulkInsert(ctx, db, "items", []string{"id", "name_2"}); err != nil {
		t.Error(err)
	}
}

func TestBulkFailureRollsBackFlushedRows(t *testing.T) {
	db, ctx := testDB(t), context.Background()
	err := db.TX(ctx, func(tx *database.DB) error {
		bulk, err := newBulkInsert(ctx, tx, "items", []string{"id", "name"})
		if err != nil {
			return err
		}
		for i := range 1200 {
			bulk.Values(i, fmt.Sprint(i))
		}
		bulk.Values(1, "duplicate")
		return bulk.Finish()
	})
	if err == nil {
		t.Fatal("duplicate insert succeeded")
	}
	var count int
	if err := db.Get(ctx, &count, `select count(*) from items`); err != nil || count != 0 {
		t.Fatalf("rollback left %d rows: %v", count, err)
	}
}
