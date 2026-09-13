package goatcounter

import (
	"context"
	"database/sql"
	"sync"

	"github.com/marvinrabe/goatcounter/internal/database"
)

var (
	keyBatchCache = &struct{ n string }{"batch cache"}
	keyLocations  = &struct{ n string }{"locations"}
)

// Dimension IDs belong to one transaction. The collector and importer resolve
// dimensions sequentially, so these maps need neither locks nor expiration.
type batchCache struct {
	tx        *sql.Tx
	agents    map[string]UserAgent
	browsers  map[string]Browser
	systems   map[string]System
	paths     map[string]Path
	refs      map[string]Ref
	campaigns map[string]Campaign
	locations map[string]Location
}

// NewBatchCache starts a fresh cache inside a collector/import transaction.
// Nothing is cached outside a transaction, or inherited by another transaction.
func NewBatchCache(ctx context.Context) context.Context {
	_, tx := database.DBSQL(ctx)
	if tx == nil {
		return ctx
	}
	return context.WithValue(ctx, keyBatchCache, &batchCache{
		tx:        tx,
		agents:    make(map[string]UserAgent),
		browsers:  make(map[string]Browser),
		systems:   make(map[string]System),
		paths:     make(map[string]Path),
		refs:      make(map[string]Ref),
		campaigns: make(map[string]Campaign),
		locations: make(map[string]Location),
	})
}

func batchCacheFor(ctx context.Context) batchCache {
	if c, ok := ctx.Value(keyBatchCache).(*batchCache); ok {
		if _, tx := database.DBSQL(ctx); tx == c.tx {
			return *c
		}
	}
	return batchCache{}
}

// Country/region records are shared across requests to avoid a database query
// for every incoming hit. Store values, not mutable receiver pointers, and keep
// uncommitted records in the batch only.
func cachedLocation(ctx context.Context, code string) (Location, bool) {
	if c := batchCacheFor(ctx).locations; c != nil {
		l, ok := c[code]
		return l, ok
	}
	if _, tx := database.DBSQL(ctx); tx == nil {
		if c, ok := ctx.Value(keyLocations).(*sync.Map); ok {
			if l, ok := c.Load(code); ok {
				return l.(Location), true
			}
		}
	}
	return Location{}, false
}

func (l Location) cache(ctx context.Context) {
	if c := batchCacheFor(ctx).locations; c != nil {
		c[l.ISO3166_2] = l
		return
	}
	if _, tx := database.DBSQL(ctx); tx == nil {
		if c, ok := ctx.Value(keyLocations).(*sync.Map); ok {
			c.Store(l.ISO3166_2, l)
		}
	}
}
