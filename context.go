package goatcounter

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
)

type contextKey uint8

const (
	keySite contextKey = iota
	keyConfig
)

// GlobalConfig holds settings shared by all requests and background jobs.
// Settings are initialized before serving; only Draining changes at runtime.
type GlobalConfig struct {
	Draining     atomic.Bool
	Timezone     datetime.Timezone
	DomainStatic string
	BasePath     string
	Dev          bool
	Sites        []Site
}

func (c *GlobalConfig) Site(name string) (Site, bool) {
	for _, s := range c.Sites {
		if strings.EqualFold(s.LinkDomain, strings.TrimSpace(name)) {
			return s, true
		}
	}
	return Site{}, false
}

// WithSite adds the site to the context.
func WithSite(ctx context.Context, s *Site) context.Context {
	return context.WithValue(ctx, keySite, s)
}

// GetSite gets the current site.
func GetSite(ctx context.Context) *Site {
	s, _ := ctx.Value(keySite).(*Site)
	return s
}

// MustGetSite behaves as GetSite(), panicking if this fails.
func MustGetSite(ctx context.Context) *Site {
	s := GetSite(ctx)
	if s == nil {
		panic("MustGetSite: no site on context")
	}
	return s
}

// NewContext attaches the database and fresh application settings and caches.
// Cancellation, deadlines, and other values are inherited from the parent.
func NewContext(ctx context.Context, db database.DB) context.Context {
	ctx = database.WithDB(ctx, db)
	ctx = context.WithValue(ctx, keyLocations, new(sync.Map))
	return NewConfig(ctx)
}

// NewConfig attaches fresh settings without requiring a database.
func NewConfig(ctx context.Context) context.Context {
	return context.WithValue(ctx, keyConfig, &GlobalConfig{})
}

// Config returns shared settings, or defaults for a standalone context.
func Config(ctx context.Context) *GlobalConfig {
	if c := ctx.Value(keyConfig); c != nil {
		return c.(*GlobalConfig)
	}
	return &GlobalConfig{}
}
