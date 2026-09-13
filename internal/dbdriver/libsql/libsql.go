// Package libsql integrates remote libSQL and local SQLite with zdb.
package libsql

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	remotelibsql "github.com/tursodatabase/libsql-client-go/libsql"
	"github.com/tursodatabase/libsql-client-go/sqliteparserutils"
	"zgo.at/zdb"
	"zgo.at/zdb/drivers"
)

func init() {
	drivers.RegisterDriver(driver{})
}

// FileConnect returns a zdb connection string for a local database path.
func FileConnect(path string) string {
	return "libsql+" + (&url.URL{Scheme: "file", Path: path}).String()
}

// Open connects to a libSQL database and initializes an empty database from
// the supplied schema. The remote client executes one statement at a time, so the
// rendered baseline is split and applied in a transaction before reconnecting
// with Files enabled for zdb's query loader.
func Open(ctx context.Context, opt zdb.ConnectOptions) (zdb.DB, error) {
	// A local SQLite file has one writer. Keep one connection to avoid
	// self-contention; shared deployments use a remote libSQL endpoint.
	if !isRemote(opt.Connect) {
		opt.MaxOpenConns = 1
		opt.MaxIdleConns = 1
	}
	files := opt.Files
	opt.Files = nil
	db, err := zdb.Connect(ctx, opt)
	if err != nil {
		return db, err
	}
	configureRemotePool(db, opt.Connect)
	if files == nil {
		return db, nil
	}

	var tables int
	err = db.Get(ctx, &tables, `select count(*) from sqlite_schema where tbl_name != 'version'`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("libsql.Open: inspect schema: %w", err)
	}
	if tables == 0 {
		if !opt.Create {
			db.Close()
			return nil, &drivers.NotExistError{Driver: "libsql", Connect: opt.Connect}
		}
		if err := createSchema(ctx, db, files); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := db.Close(); err != nil {
		return nil, err
	}

	opt.Files = files
	opt.Create = false
	db, err = zdb.Connect(ctx, opt)
	if err == nil {
		configureRemotePool(db, opt.Connect)
	}
	return db, err
}

// configureRemotePool disables idle connection reuse for remote databases.
// Remote Hrana streams expire after a short period of inactivity. Opening a
// fresh stream avoids expired-stream errors across supported server versions;
// the HTTP transport still reuses its underlying connections.
//
// This must run after zdb.Connect: zdb applies its own pool settings after the
// driver's Connect method returns, so configuring this in driver.Connect gets
// overwritten.
func configureRemotePool(db zdb.DB, connect string) {
	if !isRemote(connect) {
		return
	}
	sqlDB, _ := db.DBSQL()
	sqlDB.SetMaxIdleConns(0)
}

func isRemote(connect string) bool {
	connect = strings.TrimPrefix(connect, "libsql+")
	return strings.HasPrefix(connect, "libsql://") ||
		strings.HasPrefix(connect, "http://") ||
		strings.HasPrefix(connect, "https://")
}

func createSchema(ctx context.Context, db zdb.DB, files fs.FS) error {
	schema, err := fs.ReadFile(files, "db/schema.gotxt")
	if err != nil {
		schema, err = fs.ReadFile(files, "schema.gotxt")
	}
	if err != nil {
		return fmt.Errorf("libsql.Open: read schema: %w", err)
	}
	rendered, err := zdb.Template(zdb.DialectSQLite, string(schema))
	if err != nil {
		return fmt.Errorf("libsql.Open: render schema: %w", err)
	}
	for {
		err = db.TX(ctx, func(ctx context.Context) error {
			// Serialize simultaneous first starts before checking the schema.
			if err := zdb.Exec(ctx, `create table if not exists init_lock(id integer primary key)`); err != nil {
				return err
			}
			var tables int
			if err := zdb.Get(ctx, &tables, `select count(*) from sqlite_schema where type='table' and name not in ('init_lock','version')`); err != nil {
				return err
			}
			if tables > 0 {
				return nil
			}
			return execStatements(ctx, zdb.MustGetDB(ctx), string(rendered))
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
func execStatements(ctx context.Context, db zdb.DB, script string) error {
	statements, _ := sqliteparserutils.SplitStatement(script)
	return zdb.TX(zdb.WithDB(ctx, db), func(txctx context.Context) error {
		for _, statement := range statements {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if err := zdb.Exec(txctx, statement); err != nil {
				return err
			}
		}
		return nil
	})
}

type driver struct{}

func (driver) Name() string    { return "libsql" }
func (driver) Dialect() string { return "sqlite" }

func (driver) Connect(ctx context.Context, connect string, create bool) (*sql.DB, any, error) {
	path, local, err := localPath(connect)
	if err != nil {
		return nil, nil, err
	}
	if local {
		if err := prepareLocal(path, connect, create); err != nil {
			return nil, nil, err
		}
	}

	var db *sql.DB
	if isRemote(connect) {
		connector, err := remotelibsql.NewConnector(connect)
		if err != nil {
			return nil, nil, fmt.Errorf("libsql.Connect: %w", err)
		}
		db = sql.OpenDB(&remoteConnector{Connector: connector})
	} else {
		db, err = sql.Open("sqlite3", connect)
		if err != nil {
			return nil, nil, fmt.Errorf("libsql.Connect: %w", err)
		}
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("libsql.Connect: %w", err)
	}
	return db, nil, nil
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
		return &drivers.NotExistError{Driver: "libsql", DB: path, Connect: connect}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("libsql.Connect: create DB dir: %w", err)
	}
	return nil
}

func (driver) ErrUnique(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed") ||
		strings.Contains(s, "SQLITE_CONSTRAINT_UNIQUE") ||
		strings.Contains(s, "error code = 2067")
}

func (driver) StartTest(t testing.TB, opt *drivers.TestOptions) context.Context {
	t.Helper()
	if opt == nil {
		opt = &drivers.TestOptions{}
	}
	connect := FileConnect(filepath.Join(t.TempDir(), "goatcounter.db"))
	if opt.Connect != "" {
		connect = opt.Connect
	}
	db, err := Open(context.Background(), zdb.ConnectOptions{
		Connect:      connect,
		Create:       true,
		Files:        opt.Files,
		GoMigrations: opt.GoMigrations,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return zdb.WithDB(context.Background(), db)
}
