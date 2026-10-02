// Package testenv contains testing helpers.
package testenv

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/database"
	libsqldriver "github.com/marvinrabe/goatcounter/internal/dbdriver/libsql"
)

// Context creates a new test context.
func Context(db database.DB) context.Context {
	ctx := goatcounter.NewContext(context.Background(), db)

	s := goatcounter.Site{Key: "example.com", LinkDomain: "example.com"}
	s.Defaults()
	goatcounter.Config(ctx).Sites = []goatcounter.Site{s}
	return ctx
}

// DB starts a new database test.
func DB(t testing.TB) context.Context {
	t.Helper()
	return db(t)
}

func db(t testing.TB) context.Context {
	t.Helper()

	conn := libsqldriver.FileConnect(filepath.Join(t.TempDir(), "goatcounter.db"))
	os.Setenv("TESTENV_CONNECT", conn)

	files := os.DirFS(ModuleRoot())
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

// StoreHits stores hits through the collector. Hits from the same RemoteAddr
// and UserAgentHeader are one visitor, and one visit within 30 minutes.
func StoreHits(ctx context.Context, t testing.TB, hits ...goatcounter.Hit) {
	t.Helper()
	for _, h := range hits {
		if h.Path == "" {
			h.Path = "/"
		}
		if err := goatcounter.Collect(ctx, h); err != nil {
			t.Fatalf("StoreHits: %v", err)
		}
	}
}
