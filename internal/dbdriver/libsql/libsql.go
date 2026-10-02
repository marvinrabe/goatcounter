// Package libsql integrates remote libSQL and local SQLite with database.
package libsql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marvinrabe/goatcounter/internal/database"
	remotelibsql "github.com/tursodatabase/libsql-client-go/libsql"
	"github.com/tursodatabase/libsql-client-go/sqliteparserutils"
	_ "modernc.org/sqlite"
)

// FileConnect returns a connection string for a local database path.
func FileConnect(path string) string {
	return "libsql+" + (&url.URL{Scheme: "file", Path: path}).String()
}

// Open connects to a libSQL database and initializes an empty database from
// the supplied schema. The remote client executes one statement at a time, so the
// rendered baseline is split and applied in a transaction.
func Open(ctx context.Context, opt database.ConnectOptions) (database.DB, error) {
	// A local SQLite file has one writer. Keep one connection to avoid
	// self-contention; shared deployments use a remote libSQL endpoint.
	if !isRemote(opt.Connect) {
		opt.MaxOpenConns = 1
		opt.MaxIdleConns = 1
	}
	conn, err := openSQL(ctx, strings.TrimPrefix(opt.Connect, "libsql+"), opt.Create)
	if err != nil {
		return nil, err
	}
	if opt.MaxOpenConns == 0 {
		opt.MaxOpenConns = 16
	}
	if opt.MaxIdleConns == 0 {
		opt.MaxIdleConns = 4
	}
	// Remote Hrana streams expire after a short period of inactivity, so don't
	// keep idle connections; the HTTP transport still reuses its connections.
	if isRemote(opt.Connect) {
		opt.MaxIdleConns = 0
	}
	conn.SetMaxOpenConns(opt.MaxOpenConns)
	conn.SetMaxIdleConns(opt.MaxIdleConns)
	db := database.New(conn)
	if opt.Schema == "" {
		return db, nil
	}

	var tables, events int
	err = db.Get(ctx, &tables, `select count(*) from sqlite_schema where tbl_name not in ('version', 'init_lock')`)
	if err == nil {
		err = db.Get(ctx, &events, `select count(*) from sqlite_schema where type='table' and name='events'`)
	}
	switch {
	case err != nil:
		err = fmt.Errorf("libsql.Open: inspect schema: %w", err)
	case tables == 0 && !opt.Create:
		err = fmt.Errorf("database does not exist: %w", os.ErrNotExist)
	case tables == 0:
		err = createSchema(ctx, db, opt.Schema)
	case events == 0:
		err = errors.New("libsql.Open: the database has an old schema without the events table; use a new database")
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func isRemote(connect string) bool {
	connect = strings.TrimPrefix(connect, "libsql+")
	return strings.HasPrefix(connect, "libsql://") ||
		strings.HasPrefix(connect, "http://") ||
		strings.HasPrefix(connect, "https://")
}

func createSchema(ctx context.Context, db database.DB, schema string) error {
	var err error
	for {
		err = db.TX(ctx, func(ctx context.Context) error {
			// Serialize simultaneous first starts before checking the schema.
			if err := database.Exec(ctx, `create table if not exists init_lock(id integer primary key)`); err != nil {
				return err
			}
			var tables int
			if err := database.Get(ctx, &tables, `select count(*) from sqlite_schema where type='table' and name not in ('init_lock','version')`); err != nil {
				return err
			}
			if tables > 0 {
				return nil
			}
			return execStatements(ctx, database.MustGetDB(ctx), schema)
		})
		if err == nil || !strings.Contains(err.Error(), "database is locked") {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	if err != nil {
		return fmt.Errorf("libsql.Open: create schema: %w", err)
	}
	return nil
}

// execStatements splits and executes a SQL script in one transaction.
func execStatements(ctx context.Context, db database.DB, script string) error {
	statements, _ := sqliteparserutils.SplitStatement(script)
	return database.TX(database.WithDB(ctx, db), func(txctx context.Context) error {
		for _, statement := range statements {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if err := database.Exec(txctx, statement); err != nil {
				return err
			}
		}
		return nil
	})
}

func openSQL(ctx context.Context, connect string, create bool) (*sql.DB, error) {
	path, local, err := localPath(connect)
	if err != nil {
		return nil, err
	}
	if local {
		if err := prepareLocal(path, connect, create); err != nil {
			return nil, err
		}
	}

	var db *sql.DB
	if isRemote(connect) {
		connector, err := remotelibsql.NewConnector(connect)
		if err != nil {
			return nil, fmt.Errorf("libsql.Connect: %w", err)
		}
		db = sql.OpenDB(&remoteConnector{Connector: connector})
	} else {
		db, err = sql.Open("sqlite", connect)
		if err != nil {
			return nil, fmt.Errorf("libsql.Connect: %w", err)
		}
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("libsql.Connect: %w", err)
	}
	return db, nil
}

func localPath(connect string) (string, bool, error) {
	if strings.HasPrefix(connect, ":memory:") {
		return "", false, nil
	}
	u, err := url.Parse(connect)
	if err != nil {
		return "", false, fmt.Errorf("libsql.Connect: parse connection string: %w", err)
	}
	if u.Scheme != "file" {
		return "", false, nil
	}
	path := u.Path
	if path == "" {
		path = u.Opaque
	}
	return path, true, nil
}

func prepareLocal(path, connect string, create bool) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("libsql.Connect: %w", err)
	}
	if !create {
		if abs, absErr := filepath.Abs(path); absErr == nil {
			path = abs
		}
		return fmt.Errorf("database %s does not exist: %w", path, os.ErrNotExist)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("libsql.Connect: create DB dir: %w", err)
	}
	return nil
}
