// Package testenv contains testing helpers.
package testenv

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/database"
)

// Context creates a new test context.
func Context(db database.DB) context.Context {
	ctx := analytics.NewContext(context.Background(), db)

	s := analytics.Site{Key: "example.com", LinkDomain: "example.com"}
	s.Defaults()
	analytics.Config(ctx).Sites = []analytics.Site{s}
	return ctx
}

// DB starts a new database test.
func DB(t testing.TB) context.Context {
	t.Helper()

	db, err := database.Open(context.Background(), database.ConnectOptions{
		Connect: database.FileConnect(filepath.Join(t.TempDir(), "goatcounter.db")),
		Schema:  database.Schema,
		Create:  true,
	})
	if err != nil {
		t.Fatalf("connect to DB: %s", err)
	}

	ctx := Context(db)
	site := analytics.Config(ctx).Sites[0]
	ctx = analytics.WithSite(ctx, &site)

	t.Cleanup(func() {
		db.Close()
	})

	return ctx
}

// StoreHits stores hits through the collector. Hits from the same RemoteAddr
// and UserAgentHeader are one visitor, and one visit within 30 minutes.
func StoreHits(ctx context.Context, t testing.TB, hits ...analytics.Hit) {
	t.Helper()
	for _, h := range hits {
		if h.Path == "" {
			h.Path = "/"
		}
		if err := analytics.Collect(ctx, h); err != nil {
			t.Fatalf("StoreHits: %v", err)
		}
	}
}
