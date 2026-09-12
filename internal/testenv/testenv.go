// Package testenv contains testing helpers.
package testenv

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/cron"
	libsqldriver "github.com/marvinrabe/goatcounter/internal/dbdriver/libsql"
	"github.com/marvinrabe/goatcounter/internal/geo"
	"zgo.at/zdb"
	"zgo.at/zstd/zgo"
)

func init() {
	// Keep the extracted GeoIP database out of the source tree: geo.CacheDir is
	// relative, and under "go test" the working directory is the package being
	// tested, so every package that opens it would get its own copy. The
	// filename is content-hashed, so sharing one directory across runs and
	// packages is safe.
	geo.CacheDir = filepath.Join(os.TempDir(), "goatcounter-test-geoip")
}

// Context creates a new test context.
func Context(db zdb.DB) context.Context {
	ctx := goatcounter.NewContext(context.Background(), db)
	geodb, _ := geo.Open("")
	ctx = geo.With(ctx, geodb)

	goatcounter.Config(ctx).Domain = "test"
	s := goatcounter.Site{Key: "example.com", LinkDomain: "example.com"}
	s.Defaults(ctx)
	goatcounter.Config(ctx).Sites = []goatcounter.Site{s}
	return ctx
}

// DB starts a new database test.
func DB(t testing.TB) context.Context {
	t.Helper()
	return db(t, false, nil)
}

// DBFile is like DB(), but guarantees that the database will be written to
// disk, whereas DB() may store it in memory.
//
// You can get the connection string from the TESTENV_CONNECT environment
// variable.
func DBFile(t testing.TB) context.Context {
	t.Helper()
	return db(t, true, nil)
}

func db(t testing.TB, storeFile bool, queries *QueryCounts) context.Context {
	t.Helper()

	conn := libsqldriver.FileConnect(filepath.Join(t.TempDir(), "goatcounter.db"))
	os.Setenv("TESTENV_CONNECT", conn)

	var files fs.FS = os.DirFS(zgo.ModuleRoot())
	if queries != nil {
		files = countedFiles{files, queries}
	}
	db, err := libsqldriver.Open(context.Background(), zdb.ConnectOptions{
		Connect: conn,
		Files:   files,
		Create:  true,
	})
	if err != nil {
		t.Fatalf("connect to DB: %s", err)
	}

	ctx := Context(db)
	goatcounter.Memstore.TestInit(db)
	site := goatcounter.Config(ctx).Sites[0]
	ctx = goatcounter.WithSite(ctx, &site)
	cron.Start(ctx)

	t.Cleanup(func() {
		goatcounter.Memstore.Reset()
		cron.Stop()
		db.Close()
	})

	return ctx
}

// StoreHits is a convenient helper to store hits in the DB via Memstore and
// cron.UpdateStats().
func StoreHits(ctx context.Context, t *testing.T, wantFail bool, hits ...goatcounter.Hit) []goatcounter.Hit {
	t.Helper()

	for i := range hits {
		if hits[i].Session.IsZero() {
			hits[i].Session = goatcounter.TestSession
		}
		if hits[i].Path == "" {
			hits[i].Path = "/"
		}
	}

	goatcounter.Memstore.Append(hits...)
	hits, err := goatcounter.Memstore.Persist(ctx)
	if !wantFail && err != nil {
		t.Fatalf("testenv.StoreHits failed: %s", err)
	}
	if wantFail && err == nil {
		t.Fatal("testenv.StoreHits: no error while wantError is true")
	}

	if len(hits) > 0 {
		err = cron.UpdateStats(ctx, hits)
		if err != nil {
			t.Fatal(err)
		}
	}

	return hits
}
