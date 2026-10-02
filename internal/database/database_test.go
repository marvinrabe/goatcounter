package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { conn.Close() })
	db := New(conn)
	if err := db.Exec(context.Background(), `create table items(id integer primary key, name text)`); err != nil {
		t.Fatal(err)
	}
	return db
}
func TestNamedQueriesBindData(t *testing.T) {
	db, ctx := testDB(t), context.Background()
	attack := `'; delete from items; -- :name ?`
	if err := db.Exec(ctx, `insert into items values (?,?)`, 1, attack); err != nil {
		t.Fatal(err)
	}
	var got string
	err := db.Get(ctx, &got, `select name from items where :filter limit :limit`, map[string]any{"filter": SQL(`id in (:ids) and name=:name`), "ids": []int{1, 2}, "name": attack, "limit": 1})
	if err != nil || got != attack {
		t.Fatalf("named query = %q, %v", got, err)
	}
	if err := db.Get(ctx, &got, `select ':ignore ?' /* :missing ? */ -- :other
 where :value=1`, map[string]any{"value": 1}); err != nil || got != ":ignore ?" {
		t.Fatalf("SQL literals = %q, %v", got, err)
	}
	var count int
	if err := db.Get(ctx, &count, `select count(*) from items where id in (:ids)`, map[string]any{"ids": []int{}}); err != nil || count != 0 {
		t.Fatalf("empty IN = %d, %v", count, err)
	}
	if err := db.Get(ctx, &count, `select :missing`, map[string]any{}); err == nil {
		t.Fatal("missing parameter accepted")
	}
	if err := db.Get(ctx, &count, `select :cycle`, map[string]any{"cycle": SQL(":cycle")}); err == nil {
		t.Fatal("recursive fragment accepted")
	}
}
func TestNestedTransactionRollback(t *testing.T) {
	db, ctx := testDB(t), context.Background()
	sentinel := errors.New("abort")
	err := db.TX(ctx, func(tx DB) error {
		if err := tx.Exec(ctx, `insert into items values (1,'outer')`); err != nil {
			return err
		}
		return tx.TX(ctx, func(tx DB) error {
			if err := tx.Exec(ctx, `insert into items values (2,'inner')`); err != nil {
				return err
			}
			return sentinel
		})
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction error = %v", err)
	}
	var count int
	if err := db.Get(ctx, &count, `select count(*) from items`); err != nil || count != 0 {
		t.Fatalf("rollback left %d rows: %v", count, err)
	}
}
func TestBulkFailureRollsBackFlushedRows(t *testing.T) {
	db, ctx := testDB(t), context.Background()
	err := db.TX(ctx, func(tx DB) error {
		bulk, err := NewBulkInsert(ctx, tx, "items", []string{"id", "name"})
		if err != nil {
			return err
		}
		for i := 0; i < 1200; i++ {
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
func TestTransactionPanicRollsBack(t *testing.T) {
	db, ctx := testDB(t), context.Background()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic did not propagate")
			}
		}()
		_ = db.TX(ctx, func(tx DB) error {
			if err := tx.Exec(ctx, `insert into items values(1,'panic')`); err != nil {
				t.Fatal(err)
			}
			panic("rollback")
		})
	}()
	var count int
	if err := db.Get(ctx, &count, `select count(*) from items`); err != nil || count != 0 {
		t.Fatalf("rollback left %d rows: %v", count, err)
	}
}
