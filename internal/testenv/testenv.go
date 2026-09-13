// Package testenv contains testing helpers.
package testenv

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/cron"
	"github.com/marvinrabe/goatcounter/internal/database"
	libsqldriver "github.com/marvinrabe/goatcounter/internal/dbdriver/libsql"
	"github.com/marvinrabe/goatcounter/internal/geo"
	"github.com/marvinrabe/goatcounter/internal/testutil"
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
func Context(db database.DB) context.Context {
	ctx := goatcounter.NewContext(context.Background(), db)
	geodb, _ := geo.Open("")
	ctx = geo.With(ctx, geodb)

	s := goatcounter.Site{Key: "example.com", LinkDomain: "example.com"}
	s.Defaults()
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

	files := os.DirFS(testutil.ModuleRoot())
	if queries != nil {
		files = countedFiles{files, queries}
	}
	db, err := libsqldriver.Open(context.Background(), database.ConnectOptions{
		Connect: conn,
		Files:   files,
		Create:  true,
	})
	if err != nil {
		t.Fatalf("connect to DB: %s", err)
	}

	ctx := Context(db)
	site := goatcounter.Config(ctx).Sites[0]
	ctx = goatcounter.WithSite(ctx, &site)

	t.Cleanup(func() {
		db.Close()
	})

	return ctx
}

// StoreHits is a convenient helper to store hits in the DB via the durable collector and
// cron.UpdateStats().
func StoreHits(ctx context.Context, t *testing.T, wantFail bool, hits ...goatcounter.Hit) []goatcounter.Hit {
	t.Helper()

	for i := range hits {
		if hits[i].Session == (goatcounter.SessionID{}) {
			hits[i].Session = goatcounter.TestSession
		}
		if hits[i].Path == "" {
			hits[i].Path = "/"
		}
	}

	if err := goatcounter.EnqueueHits(ctx, hits...); err != nil {
		t.Fatal(err)
	}
	var stored []goatcounter.Hit
	var persistErr error
	for range (len(hits) / goatcounter.HitBatchSize) + 1 {
		batch, err := goatcounter.PersistHits(ctx, cron.UpdateStats)
		if err != nil {
			persistErr = err
			break
		}
		stored = append(stored, batch...)
	}
	if !wantFail && persistErr != nil {
		t.Fatalf("StoreHits: %v", persistErr)
	}
	if wantFail && persistErr == nil {
		t.Fatal("StoreHits: expected error")
	}
	hits = stored

	return hits
}
