package goatcounter_test

import (
	"testing"
	"time"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zstd/ztime"
)

func TestSiteFromConfig(t *testing.T) {
	ctx := testenv.Context(nil)
	s, ok := Config(ctx).Site("example.com")
	if !ok {
		t.Fatal("configured site not found")
	}
	s.Defaults()
	if s.Key != "example.com" || s.LinkDomain != "example.com" {
		t.Fatalf("wrong site: %#v", s)
	}
}

func TestSitesAreIsolatedByStableName(t *testing.T) {
	ctx := testenv.DB(t)
	second := Site{Key: "foobar.net", LinkDomain: "foobar.net"}
	second.Defaults()
	Config(ctx).Sites = append(Config(ctx).Sites, second)

	now := ztime.Now(ctx)
	testenv.StoreHits(ctx, t, false,
		Hit{Site: "example.com", Path: "/same", FirstVisit: true, CreatedAt: now},
		Hit{Site: "foobar.net", Path: "/same", FirstVisit: true, CreatedAt: now},
		Hit{Site: "foobar.net", Path: "/other", FirstVisit: true, CreatedAt: now},
	)

	rng := ztime.NewRange(now.Add(-time.Hour)).To(now.Add(time.Hour))
	for _, tt := range []struct {
		name  string
		total int
	}{{"example.com", 1}, {"foobar.net", 2}} {
		s, _ := Config(ctx).Site(tt.name)
		siteCtx := WithSite(ctx, &s)
		var rows HitLists
		total, _, err := rows.List(siteCtx, rng, PathFilter{}, nil, 10, GroupHourly)
		if err != nil {
			t.Fatal(err)
		}
		if total != tt.total {
			t.Errorf("%s total=%d, want %d", tt.name, total, tt.total)
		}
	}
}
