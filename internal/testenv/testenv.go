// Package testenv contains testing helpers.
package testenv

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/database"
)

// Store creates a store with an empty database, and example.com as the
// only site.
func Store(t testing.TB) *analytics.Store {
	t.Helper()

	db, err := database.Open(context.Background(), database.ConnectOptions{
		Connect: database.FileConnect(filepath.Join(t.TempDir(), "goatcounter.db")),
		Schema:  database.Schema,
		Create:  true,
	})
	if err != nil {
		t.Fatalf("connect to DB: %s", err)
	}
	t.Cleanup(func() { db.Close() })

	site := analytics.Site{Key: "example.com", LinkDomain: "example.com"}
	site.Defaults()
	return &analytics.Store{DB: db, Sites: []analytics.Site{site}}
}

// StoreHits stores hits for the first site through the collector. Hits from
// the same RemoteAddr and UserAgentHeader are one visitor, and one visit
// within 30 minutes.
func StoreHits(t testing.TB, store *analytics.Store, hits ...analytics.Hit) {
	t.Helper()
	for _, h := range hits {
		if h.Path == "" {
			h.Path = "/"
		}
		if err := store.Collect(context.Background(), store.Sites[0], h); err != nil {
			t.Fatalf("StoreHits: %v", err)
		}
	}
}
