package goatcounter_test

import (
	"testing"
	"time"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zstd/zjson"
	"zgo.at/zstd/ztest"
	"zgo.at/zstd/ztime"
)

func TestListRefsByPathID(t *testing.T) {
	ctx := testenv.DB(t)

	testenv.StoreHits(ctx, t, false,
		Hit{Path: "/x", Ref: "http://example.com", FirstVisit: true},
		Hit{Path: "/x", Ref: "http://example.com", FirstVisit: true},
		Hit{Path: "/x", Ref: "http://example.org", FirstVisit: true},
		Hit{Path: "/y", Ref: "http://example.org", FirstVisit: true})

	rng := ztime.NewRange(ztime.Now(ctx).Add(-1 * time.Hour)).To(ztime.Now(ctx).Add(1 * time.Hour))

	var have HitStats
	err := have.ListRefsByPathID(ctx, 1, rng, 10, 0)
	if err != nil {
		t.Fatal(err)
	}

	want := `{
		"more": false,
		"stats": [{
			"count": 2,
			"name": "example.com",
			"ref_scheme": "h"
		}, {
			"count": 1,
			"name": "example.org",
			"ref_scheme": "h"
		}]}`
	if d := ztest.Diff(zjson.MustMarshalString(have), want, ztest.DiffJSON); d != "" {
		t.Error(d)
	}
}

func TestListTopRefs(t *testing.T) {
	ctx := testenv.DB(t)

	testenv.StoreHits(ctx, t, false,
		Hit{Path: "/x", Ref: "http://example.com", FirstVisit: true},
		Hit{Path: "/x", Ref: "http://example.com"},
		Hit{Path: "/x", Ref: "http://example.org"},
		Hit{Path: "/y", Ref: "http://example.org", FirstVisit: true},
		Hit{Path: "/x", Ref: "http://example.org"})

	rng := ztime.NewRange(ztime.Now(ctx).Add(-1 * time.Hour)).To(ztime.Now(ctx).Add(1 * time.Hour))

	{
		var have HitStats
		err := have.ListTopRefs(ctx, rng, PathFilter{}, 10, 0)
		if err != nil {
			t.Fatal(err)
		}

		want := `{
			"more": false,
			"stats": [{
				"name": "example.org",
				"count": 1,
				"ref_scheme": "h"
			}]
		}`
		if d := ztest.Diff(zjson.MustMarshalString(have), want, ztest.DiffJSON); d != "" {
			t.Error(d)
		}
	}

	{
		var have HitStats
		err := have.ListTopRefs(ctx, rng, PathFilterFromIDs([]PathID{2}), 10, 0)
		if err != nil {
			t.Fatal(err)
		}

		want := `{
			"more": false,
			"stats": [{
				"name": "example.org",
				"count": 1,
				"ref_scheme": "h"
			}]
		}`
		if d := ztest.Diff(zjson.MustMarshalString(have), want, ztest.DiffJSON); d != "" {
			t.Error(d)
		}
	}
}
