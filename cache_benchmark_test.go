package goatcounter_test

import (
	"context"
	"fmt"
	"testing"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/geo"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

// Measure dimension resolution separately from queue/session/statistics work.
// Each iteration represents one collector batch; the cache starts cold.
func BenchmarkBatchDimensions(b *testing.B) {
	for _, distinct := range []int{1, HitBatchSize} {
		for _, cached := range []bool{false, true} {
			b.Run(fmt.Sprintf("distinct=%d/cache=%t", distinct, cached), func(b *testing.B) {
				ctx := testenv.DB(b)
				ctx = WithSite(database.WithDB(context.Background(), database.MustGetDB(ctx)), MustGetSite(ctx))
				paths, refs, agents := make([]string, distinct), make([]string, distinct), make([]string, distinct)
				lookup := func(ctx context.Context, i int) error {
					if err := (&Path{Path: paths[i]}).GetOrInsert(ctx); err != nil {
						return err
					}
					if err := (&Ref{Ref: refs[i], RefScheme: RefSchemeHTTP}).GetOrInsert(ctx); err != nil {
						return err
					}
					return (&UserAgent{UserAgent: agents[i]}).GetOrInsert(ctx)
				}
				for i := range distinct {
					paths[i] = fmt.Sprintf("/page/%d", i)
					refs[i] = fmt.Sprintf("ref%d.example", i)
					agents[i] = fmt.Sprintf("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36", 100+i)
					if err := lookup(ctx, i); err != nil {
						b.Fatal(err)
					}
				}
				ctx, queries := testenv.CountInlineQueries(ctx)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if err := database.TX(ctx, func(txctx context.Context) error {
						if cached {
							txctx = NewBatchCache(txctx)
						}
						for i := range HitBatchSize {
							if err := lookup(txctx, i%distinct); err != nil {
								return err
							}
						}
						return nil
					}); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(queries.Count("select"))/float64(b.N), "selects/batch")
			})
		}
	}
}

func BenchmarkLocationLookupCache(b *testing.B) {
	for _, cached := range []bool{false, true} {
		b.Run(fmt.Sprintf("cache=%t", cached), func(b *testing.B) {
			ctx := testenv.DB(b)
			if !cached {
				ctx = geo.With(database.WithDB(context.Background(), database.MustGetDB(ctx)), geo.Get(ctx))
			}
			if err := (&Location{}).Lookup(ctx, "51.171.91.33"); err != nil {
				b.Fatal(err)
			}
			ctx, queries := testenv.CountInlineQueries(ctx)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := (&Location{}).Lookup(ctx, "51.171.91.33"); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(queries.Count("select"))/float64(b.N), "selects/lookup")
		})
	}
}
