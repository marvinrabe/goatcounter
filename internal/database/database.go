// Package database contains the application's query and transaction helpers.
// Connections and row mapping are provided by database/sql and sqlx.
package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"io"

	"github.com/jmoiron/sqlx"
)

type DB = *Database

type Database struct {
	conn *sqlx.DB
	tx   *sqlx.Tx
	log  io.Writer
}

type ConnectOptions struct {
	Connect string
	Create  bool
	Schema  string // Creates the tables in an empty database.
}

// Schema creates GoatCounter's tables.
//
//go:embed schema.sql
var Schema string

func New(conn *sql.DB) DB {
	// The driver name only selects sqlx's "?" placeholder style; prepare()
	// expands named parameters itself.
	return &Database{conn: sqlx.NewDb(conn, "sqlite3")}
}
func (db *Database) Close() error { return db.conn.Close() }

func (db *Database) queryer() sqlx.ExtContext {
	if db.tx != nil {
		return db.tx
	}
	return db.conn
}
func (db *Database) record(query string, args []any) {
	if db.log != nil {
		fmt.Fprintf(db.log, "%s %v\n", query, args)
	}
}
func (db *Database) Get(ctx context.Context, dest any, query string, params ...any) error {
	q, args, err := db.prepare(query, params...)
	if err != nil {
		return err
	}
	db.record(q, args)
	return sqlx.GetContext(ctx, db.queryer(), dest, q, args...)
}
func (db *Database) Select(ctx context.Context, dest any, query string, params ...any) error {
	q, args, err := db.prepare(query, params...)
	if err != nil {
		return err
	}
	db.record(q, args)
	return sqlx.SelectContext(ctx, db.queryer(), dest, q, args...)
}
func (db *Database) NumRows(ctx context.Context, query string, params ...any) (int64, error) {
	q, args, err := db.prepare(query, params...)
	if err != nil {
		return 0, err
	}
	db.record(q, args)
	result, err := db.queryer().ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
func (db *Database) Exec(ctx context.Context, query string, params ...any) error {
	_, err := db.NumRows(ctx, query, params...)
	return err
}

// TX runs fn with a DB in a transaction. Inside a transaction it reuses it, so
// nested operations commit or roll back together. A panic also rolls back
// before it propagates to the caller.
func (db *Database) TX(ctx context.Context, fn func(tx DB) error) error {
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

func WithQueryLog(db DB, w io.Writer) DB { clone := *db; clone.log = w; return &clone }
