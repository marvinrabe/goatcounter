package goatcounter

import (
	"context"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/marvinrabe/goatcounter/internal/geo"
	"zgo.at/zcache/v2"
	"zgo.at/zdb"
	"zgo.at/zhttp/ctxkey"
)

// Version of GoatCounter; set at compile-time with:
//
//	-ldflags="-X github.com/marvinrabe/goatcounter.Version=…"
var Version = "dev"

func getCommit() (string, time.Time, bool) {
	var (
		rev     string
		last    time.Time
		dirty   bool
		info, _ = debug.ReadBuildInfo()
	)
	for _, kv := range info.Settings {
		switch kv.Key {
		case "vcs.revision":
			rev = kv.Value
		case "vcs.time":
			last, _ = time.Parse(time.RFC3339, kv.Value)
		case "vcs.modified":
			dirty = kv.Value == "true"
		}
	}
	return rev, last, dirty
}

func init() {
	if Version == "" || Version == "dev" {
		// Only calculate the version if not explicitly overridden with:
		//	-ldflags="-X github.com/marvinrabe/goatcounter.Version=$tag"
		// which is done for release builds.
		if rev, last, dirty := getCommit(); rev != "" {
			Version = rev[:12] + "_" + last.Format("2006-01-02T15:04:05Z0700")
			if dirty {
				Version += "-dev"
			}
		}
	}
}

var (
	keyCacheUA        = &struct{ n string }{""}
	keyCacheBrowsers  = &struct{ n string }{""}
	keyCacheSystems   = &struct{ n string }{""}
	keyCachePaths     = &struct{ n string }{""}
	keyCacheRefs      = &struct{ n string }{""}
	keyCacheLoc       = &struct{ n string }{""}
	keyCacheCampaigns = &struct{ n string }{""}

	keyConfig = &struct{ n string }{""}
	keyHost   = &struct{ n string }{""}
)

type GlobalConfig struct {
	Draining     atomic.Bool
	Timezone     Timezone
	Domain       string
	DomainStatic string
	DomainCount  string
	BasePath     string
	URLStatic    string
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
	return context.WithValue(ctx, ctxkey.Site, s)
}

// WithHost adds the host this request was served from to the context.
func WithHost(ctx context.Context, host string) context.Context {
	return context.WithValue(ctx, keyHost, host)
}

// Host gets the host this request was served from; empty if not set (e.g. in
// cron jobs or the CLI).
func Host(ctx context.Context) string {
	h, _ := ctx.Value(keyHost).(string)
	return h
}

// GetSite gets the current site.
func GetSite(ctx context.Context) *Site {
	s, _ := ctx.Value(ctxkey.Site).(*Site)
	return s
}

// MustGetSite behaves as GetSite(), panicking if this fails.
func MustGetSite(ctx context.Context) *Site {
	s, ok := ctx.Value(ctxkey.Site).(*Site)
	if !ok {
		panic("MustGetSite: no site on context")
	}
	return s
}

// NewContext creates a new context with all values set.
func NewContext(ctx context.Context, db zdb.DB) context.Context {
	n := zdb.WithDB(context.Background(), db)
	n = geo.With(n, geo.Get(ctx))
	n = NewCache(n)
	n = NewConfig(n)
	return n
}

func NewCache(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, keyCacheUA, zcache.New[string, UserAgent](30*time.Minute, 5*time.Minute))
	ctx = context.WithValue(ctx, keyCacheBrowsers, zcache.New[string, Browser](1*time.Hour, 5*time.Minute))
	ctx = context.WithValue(ctx, keyCacheSystems, zcache.New[string, System](1*time.Hour, 5*time.Minute))
	ctx = context.WithValue(ctx, keyCachePaths, zcache.New[string, Path](1*time.Hour, 5*time.Minute))
	ctx = context.WithValue(ctx, keyCacheRefs, zcache.New[string, Ref](1*time.Hour, 5*time.Minute))
	ctx = context.WithValue(ctx, keyCacheLoc, zcache.New[string, *Location](zcache.NoExpiration, zcache.NoExpiration))
	ctx = context.WithValue(ctx, keyCacheCampaigns, zcache.New[string, *Campaign](24*time.Hour, 15*time.Minute))
	return ctx
}

// NewBatchCache isolates dimension IDs to the lifetime of one transaction.
func NewBatchCache(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, keyCacheUA, zcache.New[string, UserAgent](zcache.NoExpiration, zcache.NoExpiration))
	ctx = context.WithValue(ctx, keyCacheBrowsers, zcache.New[string, Browser](zcache.NoExpiration, zcache.NoExpiration))
	ctx = context.WithValue(ctx, keyCacheSystems, zcache.New[string, System](zcache.NoExpiration, zcache.NoExpiration))
	ctx = context.WithValue(ctx, keyCachePaths, zcache.New[string, Path](zcache.NoExpiration, zcache.NoExpiration))
	ctx = context.WithValue(ctx, keyCacheRefs, zcache.New[string, Ref](zcache.NoExpiration, zcache.NoExpiration))
	ctx = context.WithValue(ctx, keyCacheLoc, zcache.New[string, *Location](zcache.NoExpiration, zcache.NoExpiration))
	ctx = context.WithValue(ctx, keyCacheCampaigns, zcache.New[string, *Campaign](zcache.NoExpiration, zcache.NoExpiration))
	return ctx
}

func NewConfig(ctx context.Context) context.Context {
	return context.WithValue(ctx, keyConfig, &GlobalConfig{})
}

func Config(ctx context.Context) *GlobalConfig {
	if c := ctx.Value(keyConfig); c != nil {
		return c.(*GlobalConfig)
	}
	return &GlobalConfig{}
}

func cacheUA(ctx context.Context) *zcache.Cache[string, UserAgent] {
	if c := ctx.Value(keyCacheUA); c != nil {
		return c.(*zcache.Cache[string, UserAgent])
	}
	return zcache.New[string, UserAgent](0, 0)
}
func cacheBrowsers(ctx context.Context) *zcache.Cache[string, Browser] {
	if c := ctx.Value(keyCacheBrowsers); c != nil {
		return c.(*zcache.Cache[string, Browser])
	}
	return zcache.New[string, Browser](0, 0)
}
func cacheSystems(ctx context.Context) *zcache.Cache[string, System] {
	if c := ctx.Value(keyCacheSystems); c != nil {
		return c.(*zcache.Cache[string, System])
	}
	return zcache.New[string, System](0, 0)
}
func cachePaths(ctx context.Context) *zcache.Cache[string, Path] {
	if c := ctx.Value(keyCachePaths); c != nil {
		return c.(*zcache.Cache[string, Path])
	}
	return zcache.New[string, Path](0, 0)
}
func cacheRefs(ctx context.Context) *zcache.Cache[string, Ref] {
	if c := ctx.Value(keyCacheRefs); c != nil {
		return c.(*zcache.Cache[string, Ref])
	}
	return zcache.New[string, Ref](0, 0)
}
func cacheLoc(ctx context.Context) *zcache.Cache[string, *Location] {
	if c := ctx.Value(keyCacheLoc); c != nil {
		return c.(*zcache.Cache[string, *Location])
	}
	return zcache.New[string, *Location](0, 0)
}
func cacheCampaigns(ctx context.Context) *zcache.Cache[string, *Campaign] {
	if c := ctx.Value(keyCacheCampaigns); c != nil {
		return c.(*zcache.Cache[string, *Campaign])
	}
	return zcache.New[string, *Campaign](0, 0)
}
