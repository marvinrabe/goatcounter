// Package database connects to a remote libSQL or a local SQLite database,
// and creates the tables that don't exist yet. Queries use SQLite's
// parameters, ? or :name with sql.Named, and sqlx scans the rows into structs
// by their db tags.
package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type DB struct {
	conn *sqlx.DB
	tx   *sqlx.Tx
	log  io.Writer
}

// Open connects to a remote libSQL database (a libsql://, http://, or
// https:// URL), or to a local SQLite file, which is created if it doesn't
// exist.
func Open(ctx context.Context, connect string) (*DB, error) {
	var conn *sql.DB
	if isRemote(connect) {
		connector, err := libsql.NewConnector(connect)
		if err != nil {
			return nil, fmt.Errorf("database.Open: %w", err)
		}
		conn = sql.OpenDB(connector)
		// Remote Hrana streams expire after a short period of inactivity, so
		// don't keep idle connections; the HTTP transport still reuses its
		// connections.
		conn.SetMaxOpenConns(16)
		conn.SetMaxIdleConns(0)
	} else {
		path, err := filepath.Abs(connect)
		if err != nil {
			return nil, fmt.Errorf("database.Open: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("database.Open: %w", err)
		}
		// Wait for other processes that are writing, rather than failing.
		dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=busy_timeout(5000)"}).String()
		conn, err = sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("database.Open: %w", err)
		}
		// A SQLite file has one writer; more connections only contend.
		conn.SetMaxOpenConns(1)
	}

	db := &DB{conn: sqlx.NewDb(conn, "sqlite")}
	if err := db.Exec(ctx, schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("database.Open: create tables: %w", err)
	}
	return db, nil
}

func isRemote(connect string) bool {
	return strings.HasPrefix(connect, "libsql://") ||
		strings.HasPrefix(connect, "http://") ||
		strings.HasPrefix(connect, "https://")
}

func (db *DB) Close() error { return db.conn.Close() }

// WithQueryLog returns a copy of db that writes every query to w.
func (db *DB) WithQueryLog(w io.Writer) *DB { clone := *db; clone.log = w; return &clone }

func (db *DB) queryer() sqlx.ExtContext {
	if db.tx != nil {
		return db.tx
	}
	return db.conn
}

func (db *DB) record(query string, args []any) {
	if db.log != nil {
		fmt.Fprintf(db.log, "%s %v\n", query, args)
	}
}

// Get scans the first row into dest.
func (db *DB) Get(ctx context.Context, dest any, query string, args ...any) error {
	db.record(query, args)
	return sqlx.GetContext(ctx, db.queryer(), dest, query, args...)
}

// Select scans all rows into dest, which is a pointer to a slice.
func (db *DB) Select(ctx context.Context, dest any, query string, args ...any) error {
	db.record(query, args)
	return sqlx.SelectContext(ctx, db.queryer(), dest, query, args...)
}

func (db *DB) Exec(ctx context.Context, query string, args ...any) error {
	db.record(query, args)
	_, err := db.queryer().ExecContext(ctx, query, args...)
	return err
}

// TX runs fn in a transaction, which is committed if fn returns nil and
// rolled back otherwise, also on a panic. In a transaction it uses the same
// one.
func (db *DB) TX(ctx context.Context, fn func(tx *DB) error) error {
	if db.tx != nil {
		return fn(db)
	}
	tx, err := db.conn.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	child := *db
	child.tx = tx
	if err := fn(&child); err != nil {
		return err
	}
	return tx.Commit()
}
