package goatcounter_test

import (
	"context"
	"testing"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zdb"
	"zgo.at/zstd/zint"
	"zgo.at/zstd/ztime"
)

func TestPersistDoesNotReadSiteMetadata(t *testing.T) {
	ctx := testenv.DB(t)
	ctx, queries := testenv.CountInlineQueries(ctx)
	for range 10 {
		Memstore.Append(Hit{Path: "/one", FirstVisit: true})
	}
	hits, err := Memstore.Persist(ctx)
	if err != nil || len(hits) != 10 {
		t.Fatalf("persisted %d hits: %v", len(hits), err)
	}
	if n := queries.Count(`select unixepoch(min(datetime(created_at))) from hits where site=?`); n != 0 {
		t.Errorf("per-hit site metadata reads = %d, want 0", n)
	}
}

func TestMemstore(t *testing.T) {
	ctx := testenv.DB(t)
	site := MustGetSite(ctx)
	site.Settings.Collect.Set(CollectHits)
	if err := site.Update(ctx); err != nil {
		t.Fatal(err)
	}

	for range 2000 {
		Memstore.Append(gen(ctx))
	}

	_, err := Memstore.Persist(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var count int
	err = zdb.Get(ctx, &count, `select count(*) from hits`)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2000 {
		t.Errorf("wrong count; wanted 2000 but have %d", count)
	}
}

func gen(ctx context.Context) Hit {
	return Hit{
		Session:         TestSession,
		Path:            "/test",
		Ref:             "https://example.com/test",
		UserAgentHeader: "test",
	}
}

func TestNextUUID(t *testing.T) {
	want := `11223344556677-8899aabbccddef01
11223344556677-8899aabbccddef02
11223344556677-8899aabbccddef03
11223344556677-8899aabbccddeeff`

	t.Run("", func(t *testing.T) {
		testenv.DB(t)

		have := Memstore.SessionID().Format(16) + "\n" +
			Memstore.SessionID().Format(16) + "\n" +
			Memstore.SessionID().Format(16) + "\n" +
			TestSession.Format(16)
		if have != want {
			t.Errorf("wrong:\n%s", have)
		}
	})

	t.Run("", func(t *testing.T) {
		testenv.DB(t)

		have := Memstore.SessionID().Format(16) + "\n" +
			Memstore.SessionID().Format(16) + "\n" +
			Memstore.SessionID().Format(16) + "\n" +
			TestSession.Format(16)
		if have != want {
			t.Errorf("wrong after reset:\n%s", have)
		}
	})
}

func TestMemstoreCollect(t *testing.T) {
	t.Skip("collection settings were removed; all data is always collected")
	all := func() zint.Bitflag16 {
		s := SiteSettings{}
		s.Defaults(context.Background())
		s.Collect.Set(CollectHits)
		return s.Collect
	}()

	tests := []struct {
		collect        zint.Bitflag16
		collectRegions Strings
		want           string
	}{
		{all, Strings{}, `
			session                           path    ref          ref_scheme  width  location  first_visit
			00112233445566778899aabbccddeeff  /test   example.com  h           5      NL        0
			00112233445566778899aabbccddeeff  /other  xxx          c           5      ID-BA     1
		`},

		{CollectNothing, Strings{}, `
			session  path  ref  ref_scheme  width  location  first_visit
		`},

		{all ^ CollectLocationRegion, Strings{}, `
			session                           path    ref          ref_scheme  width  location  first_visit
			00112233445566778899aabbccddeeff  /test   example.com  h           5      NL        0
			00112233445566778899aabbccddeeff  /other  xxx          c           5      ID        1
		`},

		{all, Strings{"US"}, `
			session                           path    ref          ref_scheme  width  location  first_visit
			00112233445566778899aabbccddeeff  /test   example.com  h           5      NL        0
			00112233445566778899aabbccddeeff  /other  xxx          c           5      ID        1
		`},
		{all, Strings{"ID"}, `
			session                           path    ref          ref_scheme  width  location  first_visit
			00112233445566778899aabbccddeeff  /test   example.com  h           5      NL        0
			00112233445566778899aabbccddeeff  /other  xxx          c           5      ID-BA     1
		`},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			ctx := testenv.DB(t)
			ctx = ztime.WithNow(ctx, ztime.FromString("2020-06-18"))

			site := MustGetSite(ctx)
			site.Settings.Collect = tt.collect
			site.Settings.CollectRegions = tt.collectRegions
			if err := site.Update(ctx); err != nil {
				t.Fatal(err)
			}

			testenv.StoreHits(ctx, t, false, Hit{
				Path:     "/test",
				Ref:      "https://example.com",
				Location: "NL",
				Size:     Floats{5, 6, 7},
			}, Hit{
				Path:       "/other",
				Query:      "ref=xxx",
				Location:   "ID-BA",
				Size:       Floats{5, 6, 7},
				FirstVisit: true,
			})

			have := zdb.DumpString(ctx, `
				select session, paths.path, refs.ref, refs.ref_scheme, width, location, first_visit
				from hits
				join paths using (path_id)
				left join refs  using (ref_id)
			`)
			if d := zdb.Diff(have, tt.want); d != "" {
				t.Error(d)
			}
		})
	}
}
