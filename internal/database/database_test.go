package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Exec(context.Background(), `create table items(id integer primary key, name text)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestIsRemote(t *testing.T) {
	for connect, want := range map[string]bool{
		"libsql://example.com":     true,
		"https://example.com":      true,
		"http://example.com":       true,
		"/data/goatcounter.db":     false,
		"goatcounter-data/test.db": false,
	} {
		if have := isRemote(connect); have != want {
			t.Errorf("isRemote(%q) = %t; want %t", connect, have, want)
		}
	}
}

func TestOpenCreatesTables(t *testing.T) {
	ctx := context.Background()
	// A relative path, in a directory that doesn't exist yet.
	t.Chdir(t.TempDir())
	path := filepath.Join("data", "goatcounter.db")
	for range 2 { // Opening again keeps the tables.
		db, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := db.Get(ctx, &n, `select count(*) from sqlite_schema where type = 'table' and name in ('events', 'salts')`); err != nil || n != 2 {
			t.Fatalf("tables = %d, %v", n, err)
		}
		if err := db.Exec(ctx, `insert into salts (day, salt) values (?, ?)
			on conflict do nothing`, "2026-10-02", []byte("salt")); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestConcurrentOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	errs := make(chan error, 3)
	for range 3 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			db, err := Open(ctx, path)
			if err == nil {
				db.Close()
			}
			errs <- err
		}()
	}
	for range 3 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

func TestNamedArgs(t *testing.T) {
	db, ctx := testDB(t), context.Background()
	attack := `'; delete from items; -- :name ?`
	if err := db.Exec(ctx, `insert into items values (:id, :name)`,
		sql.Named("id", 1), sql.Named("name", attack)); err != nil {
		t.Fatal(err)
	}
	var have string
	if err := db.Get(ctx, &have, `select name from items where id = :id and name = :name`,
		sql.Named("name", attack), sql.Named("id", 1), sql.Named("unused", 2)); err != nil || have != attack {
		t.Fatalf("named = %q, %v", have, err)
	}
	if err := db.Get(ctx, &have, `select ':name ?' -- :other`); err != nil || have != ":name ?" {
		t.Fatalf("literal = %q, %v", have, err)
	}
	if err := db.Get(ctx, &have, `select :missing`); err == nil {
		t.Fatal("missing parameter accepted")
	}
}

func TestNestedTransactionRollback(t *testing.T) {
	db, ctx := testDB(t), context.Background()
	sentinel := errors.New("abort")
	err := db.TX(ctx, func(tx *DB) error {
		if err := tx.Exec(ctx, `insert into items values (1, 'outer')`); err != nil {
			return err
		}
		return tx.TX(ctx, func(tx *DB) error {
			if err := tx.Exec(ctx, `insert into items values (2, 'inner')`); err != nil {
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

func TestTransactionPanicRollsBack(t *testing.T) {
	db, ctx := testDB(t), context.Background()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic did not propagate")
			}
		}()
		_ = db.TX(ctx, func(tx *DB) error {
			if err := tx.Exec(ctx, `insert into items values (1, 'panic')`); err != nil {
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
