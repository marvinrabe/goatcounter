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

	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "goatcounter.db"))
	if err != nil {
		t.Fatalf("connect to DB: %s", err)
	}
	t.Cleanup(func() { db.Close() })

	site := analytics.Site{Key: "example.com", LinkDomain: "example.com"}
	site.Defaults()
	return &analytics.Store{DB: db, Sites: []analytics.Site{site}}
}

// StoreEvents stores events for the first site through the collector. Events
// from the same RemoteAddr and UserAgentHeader are one visitor, and one visit
// within 30 minutes.
func StoreEvents(t testing.TB, store *analytics.Store, events ...analytics.Event) {
	t.Helper()
	for _, e := range events {
		if e.Path == "" {
			e.Path = "/"
		}
		if err := store.Collect(context.Background(), store.Sites[0], e); err != nil {
			t.Fatalf("StoreEvents: %v", err)
		}
	}
}
