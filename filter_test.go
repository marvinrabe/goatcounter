package goatcounter_test

import (
	"fmt"
	"testing"
	"time"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func TestFilterMatch(t *testing.T) {
	tests := []struct {
		query, path string
		event, want bool
	}{
		{"", "/hello", false, true},
		{"e", "/hello", false, true},
		{"/h in:path", "/hello", false, true},
		{", world in:path", "/hello", false, false},
		{"/h in:path at:end", "/hello", false, false},
		{"/h in:path at:start", "/hello", false, true},
		{"/h in:path at:start at:end", "/hello", false, false},
		{"/hello in:path at:start at:end", "/hello", false, true},

		{"HELLO", "/hello", false, true},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			have := Filter{Query: tt.query}.Match(tt.path, tt.event)
			if have != tt.want {
				t.Error(tt.query)
			}

			have = Filter{Query: tt.query + " :not"}.Match(tt.path, tt.event)
			if have == tt.want {
				t.Error(":NOT →", tt.query)
			}
		})
	}
}

func TestCachedFilterAvoidsScanAndFrequentWrites(t *testing.T) {
	ctx, queries := testenv.DBWithQueryFileCounts(t)
	paths := testenv.StoreHits(ctx, t, false,
		Hit{Path: "/keep", FirstVisit: true}, Hit{Path: "/exclude", FirstVisit: true})
	f := Filter{Query: "/keep", Invert: true}
	if err := f.Insert(ctx, []PathID{paths[1].PathID}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	ctx = datetime.WithNow(ctx, now)
	if err := database.Exec(ctx, `update filters set last_used_at=?`, now); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(ctx, `create trigger no_filter_writes before update on filters
		begin select raise(abort, 'unexpected filter update'); end`); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		filter, err := PathFilterFromQuery(ctx, "/KEEP")
		if err != nil {
			t.Fatal(err)
		}
		sql, params := filter.SQL(ctx, "paths")
		var matched []PathID
		if err := database.Select(ctx, &matched, "select path_id from paths where "+string(sql), params); err != nil {
			t.Fatal(err)
		}
		if len(matched) != 1 || matched[0] != paths[0].PathID {
			t.Fatalf("cached inverted filter matched %v", matched)
		}
	}
	if n := queries.Count("paths.PathFilter"); n != 0 {
		t.Errorf("cached filter scanned paths %d times", n)
	}

	if err := database.Exec(ctx, `drop trigger no_filter_writes`); err != nil {
		t.Fatal(err)
	}
	ctx = datetime.WithNow(ctx, now.Add(time.Hour))
	if _, err := PathFilterFromQuery(ctx, "/keep"); err != nil {
		t.Fatal(err)
	}
	if err := f.ByQuery(ctx, "/keep"); err != nil {
		t.Fatal(err)
	}
	if !f.LastUsedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("old filter was not touched: %v", f.LastUsedAt)
	}
}

func TestPathChangesInvalidateFilters(t *testing.T) {
	for _, operation := range []string{"purge", "merge"} {
		t.Run(operation, func(t *testing.T) {
			ctx := testenv.DB(t)
			hits := testenv.StoreHits(ctx, t, false,
				Hit{Path: "/one", FirstVisit: true}, Hit{Path: "/two", FirstVisit: true})
			f := Filter{Query: "/one"}
			if err := f.Insert(ctx, []PathID{hits[0].PathID}); err != nil {
				t.Fatal(err)
			}
			var err error
			if operation == "purge" {
				err = (&Hits{}).Purge(ctx, []PathID{hits[0].PathID})
			} else {
				err = (Path{ID: hits[1].PathID}).Merge(ctx, Paths{{ID: hits[0].PathID}})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := f.ByQuery(ctx, "/one"); !database.ErrNoRows(err) {
				t.Fatalf("filter survived %s: %v", operation, err)
			}
			var count int
			if err := database.Get(ctx, &count, `select count(*) from filter_paths`); err != nil || count != 0 {
				t.Fatalf("stale cached path IDs: %d, %v", count, err)
			}
		})
	}
}

func TestCachedFiltersAreScopedBySite(t *testing.T) {
	ctx := testenv.DB(t)
	second := Site{Key: "second.example", LinkDomain: "second.example"}
	second.Defaults()
	Config(ctx).Sites = append(Config(ctx).Sites, second)
	sites := Config(ctx).Sites

	// More than 10,000 matches makes PathFilterFromQuery persist the filter.
	paths, err := database.NewBulkInsert(ctx, "paths", []string{"site", "path"})
	if err != nil {
		t.Fatal(err)
	}
	for _, site := range sites {
		for i := range 10_001 {
			paths.Values(site.Key, fmt.Sprintf("/item/%d", i))
		}
	}
	if err := paths.Finish(); err != nil {
		t.Fatal(err)
	}

	filters := make([]Filter, len(sites))
	for i, site := range sites {
		sctx := WithSite(ctx, &site)
		for _, query := range []string{"/item/", "/ITEM/"} {
			filter, err := PathFilterFromQuery(sctx, query)
			if err != nil {
				t.Fatal(err)
			}
			sql, params := filter.SQL(sctx, "paths")
			var count int
			if err := database.Get(sctx, &count, "select count(*) from paths where "+string(sql), params); err != nil {
				t.Fatal(err)
			}
			if count != 10_001 {
				t.Fatalf("%s: got %d matches, want 10001", site.Key, count)
			}
		}
		if err := filters[i].ByQuery(sctx, "/item/"); err != nil {
			t.Fatal(err)
		}
	}
	if filters[0].FilterID == filters[1].FilterID {
		t.Fatal("sites share a cached filter")
	}

	// New paths must only update the matching site's cached filter.
	sctx := WithSite(ctx, &second)
	path := Path{Path: "/item/new"}
	if err := path.GetOrInsert(sctx); err != nil {
		t.Fatal(err)
	}
	for i, site := range sites {
		var count int
		if err := database.Get(ctx, &count, `select count(*) from filter_paths where filter_id=?`, filters[i].FilterID); err != nil {
			t.Fatal(err)
		}
		if want := 10_001 + i; count != want {
			t.Errorf("%s: got %d cached paths, want %d", site.Key, count, want)
		}
	}
	if err := second.DeleteAll(sctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.Get(ctx, &count, `select count(*) from filter_paths where filter_id=?`, filters[1].FilterID); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("deleted site still has %d cached paths", count)
	}
	if err := filters[0].ByQuery(ctx, "/item/"); err != nil {
		t.Fatalf("deleting another site removed the first site's filter: %v", err)
	}
	if err := filters[1].ByQuery(sctx, "/item/"); !database.ErrNoRows(err) {
		t.Fatalf("deleted site's filter: got %v, want no rows", err)
	}
}
