// Package database contains the application's query and transaction helpers.
// Connections and row mapping are provided by database/sql and sqlx.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/jmoiron/sqlx"
)

type DB = *Database

type Database struct {
	conn     *sqlx.DB
	tx       *sqlx.Tx
	files    fs.FS
	recorder interface {
		Record(time.Duration, string, []any)
	}
	log io.Writer
}

type ConnectOptions struct {
	Connect                    string
	Create                     bool
	Files                      fs.FS
	MaxOpenConns, MaxIdleConns int
}

func New(conn *sql.DB, files fs.FS) DB {
	return &Database{conn: sqlx.NewDb(conn, "sqlite3"), files: files}
}
func (db *Database) Close() error { return db.conn.Close() }
func (db *Database) DBSQL() (*sql.DB, *sql.Tx) {
	if db.tx != nil {
		return db.conn.DB, db.tx.Tx
	}
	return db.conn.DB, nil
}

type contextKey struct{}

func WithDB(ctx context.Context, db DB) context.Context {
	return context.WithValue(ctx, contextKey{}, db)
}
func MustGetDB(ctx context.Context) DB {
	db, ok := ctx.Value(contextKey{}).(DB)
	if !ok || db == nil {
		panic("database: no connection in context")
	}
	return db
}
func DBSQL(ctx context.Context) (*sql.DB, *sql.Tx) { return MustGetDB(ctx).DBSQL() }
func ErrNoRows(err error) bool                     { return errors.Is(err, sql.ErrNoRows) }

func (db *Database) queryer() sqlx.ExtContext {
	if db.tx != nil {
		return db.tx
	}
	return db.conn
}
func (db *Database) record(start time.Time, query string, args []any) {
	if db.recorder != nil {
		db.recorder.Record(time.Since(start), query, args)
	}
	if db.log != nil {
		fmt.Fprintf(db.log, "%s %v\n", query, args)
	}
}
func (db *Database) Get(ctx context.Context, dest any, query string, params ...any) error {
	q, args, err := db.prepare(query, params...)
	if err != nil {
		return err
	}
	defer db.record(time.Now(), q, args)
	return sqlx.GetContext(ctx, db.queryer(), dest, q, args...)
}
func Get(ctx context.Context, dest any, query string, params ...any) error {
	return MustGetDB(ctx).Get(ctx, dest, query, params...)
}
func Select(ctx context.Context, dest any, query string, params ...any) error {
	db := MustGetDB(ctx)
	q, args, err := db.prepare(query, params...)
	if err != nil {
		return err
	}
	defer db.record(time.Now(), q, args)
	return sqlx.SelectContext(ctx, db.queryer(), dest, q, args...)
}
func NumRows(ctx context.Context, query string, params ...any) (int64, error) {
	db := MustGetDB(ctx)
	q, args, err := db.prepare(query, params...)
	if err != nil {
		return 0, err
	}
	defer db.record(time.Now(), q, args)
	result, err := db.queryer().ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
func Exec(ctx context.Context, query string, params ...any) error {
	_, err := NumRows(ctx, query, params...)
	return err
}

// TX reuses an active transaction, so nested operations commit or roll back
// together. A panic also rolls back before it propagates to the caller.
func (db *Database) TX(ctx context.Context, fn func(context.Context) error) error {
	if db.tx != nil {
		return fn(WithDB(ctx, db))
	}
	tx, err := db.conn.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	child := *db
	child.tx = tx
	if err := fn(WithDB(ctx, &child)); err != nil {
		return err
	}
	return tx.Commit()
}
func TX(ctx context.Context, fn func(context.Context) error) error { return MustGetDB(ctx).TX(ctx, fn) }

func NewMetricsDB(db DB, r interface {
	Record(time.Duration, string, []any)
}) DB {
	clone := *db
	clone.recorder = r
	return &clone
}

func WithQueryLog(db DB, w io.Writer) DB { clone := *db; clone.log = w; return &clone }
