package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"testing/fstest"

	_ "github.com/mattn/go-sqlite3"
)

func testDB(t *testing.T) context.Context {
	t.Helper()
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { conn.Close() })
	db := New(conn, fstest.MapFS{"query/find.sql": {Data: []byte(`select name from items where :filter {{if .limit}}limit :limit{{end}}`)}})
	ctx := WithDB(context.Background(), db)
	if err := Exec(ctx, `create table items(id integer primary key, name text)`); err != nil {
		t.Fatal(err)
	}
	return ctx
}
func TestNamedQueriesBindData(t *testing.T) {
	ctx := testDB(t)
	attack := `'; delete from items; -- :name ?`
	if err := Exec(ctx, `insert into items values (?,?)`, 1, attack); err != nil {
		t.Fatal(err)
	}
	var got string
	err := Get(ctx, &got, "load:find", map[string]any{"filter": SQL(`id in (:ids) and name=:name`), "ids": []int{1, 2}, "name": attack, "limit": 1})
	if err != nil || got != attack {
		t.Fatalf("named query = %q, %v", got, err)
	}
	if err := Get(ctx, &got, `select ':ignore ?' /* :missing ? */ -- :other
 where :value=1`, map[string]any{"value": 1}); err != nil || got != ":ignore ?" {
		t.Fatalf("SQL literals = %q, %v", got, err)
	}
	var count int
	if err := Get(ctx, &count, `select count(*) from items where id in (:ids)`, map[string]any{"ids": []int{}}); err != nil || count != 0 {
		t.Fatalf("empty IN = %d, %v", count, err)
	}
	if err := Get(ctx, &count, `select :missing`, map[string]any{}); err == nil {
		t.Fatal("missing parameter accepted")
	}
	if err := Get(ctx, &count, `select :cycle`, map[string]any{"cycle": SQL(":cycle")}); err == nil {
		t.Fatal("recursive fragment accepted")
	}
}
func TestNestedTransactionRollback(t *testing.T) {
	ctx := testDB(t)
	sentinel := errors.New("abort")
	err := TX(ctx, func(ctx context.Context) error {
		if err := Exec(ctx, `insert into items values (1,'outer')`); err != nil {
			return err
		}
		return TX(ctx, func(ctx context.Context) error {
			if err := Exec(ctx, `insert into items values (2,'inner')`); err != nil {
				return err
			}
			return sentinel
		})
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction error = %v", err)
	}
	var count int
	if err := Get(ctx, &count, `select count(*) from items`); err != nil || count != 0 {
		t.Fatalf("rollback left %d rows: %v", count, err)
	}
}
func TestBulkFailureRollsBackFlushedRows(t *testing.T) {
	ctx := testDB(t)
	err := TX(ctx, func(ctx context.Context) error {
		bulk, err := NewBulkInsert(ctx, "items", []string{"id", "name"})
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
	if err := Get(ctx, &count, `select count(*) from items`); err != nil || count != 0 {
		t.Fatalf("rollback left %d rows: %v", count, err)
	}
}
func TestTransactionPanicRollsBack(t *testing.T) {
	ctx := testDB(t)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic did not propagate")
			}
		}()
		_ = TX(ctx, func(ctx context.Context) error {
			if err := Exec(ctx, `insert into items values(1,'panic')`); err != nil {
				t.Fatal(err)
			}
			panic("rollback")
		})
	}()
	var count int
	if err := Get(ctx, &count, `select count(*) from items`); err != nil || count != 0 {
		t.Fatalf("rollback left %d rows: %v", count, err)
	}
}
