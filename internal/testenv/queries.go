package testenv

import (
	"context"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter/internal/database"
)

// QueryCounts records inline SQL or successful query-file reads in tests.
type QueryCounts struct {
	queries sync.Map
}

// Count returns the number of operations containing the given SQL or file name.
func (q *QueryCounts) Count(fragment string) int64 {
	var count int64
	q.queries.Range(func(key, value any) bool {
		if strings.Contains(key.(string), fragment) {
			count += value.(*atomic.Int64).Load()
		}
		return true
	})
	return count
}

func (q *QueryCounts) add(query string) {
	n, _ := q.queries.LoadOrStore(query, new(atomic.Int64))
	n.(*atomic.Int64).Add(1)
}

func (q *QueryCounts) Record(_ time.Duration, query string, _ []any) { q.add(query) }

// CountInlineQueries records each SQL statement after query expansion.
func CountInlineQueries(ctx context.Context) (context.Context, *QueryCounts) {
	counts := new(QueryCounts)
	return database.WithDB(ctx, database.NewMetricsDB(database.MustGetDB(ctx), counts)), counts
}

// DBWithQueryFileCounts tracks named query file reads.
func DBWithQueryFileCounts(t testing.TB) (context.Context, *QueryCounts) {
	t.Helper()
	counts := new(QueryCounts)
	return db(t, false, counts), counts
}

type countedFiles struct {
	fs.FS
	counts *QueryCounts
}

func (f countedFiles) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err == nil {
		f.counts.add(name)
	}
	return file, err
}
