package testenv

import (
	"context"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"zgo.at/zdb"
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

// CountInlineQueries uses zdb's recorder. Its wrapper cannot load query files;
// use DBWithQueryFileCounts for code that executes named queries.
func CountInlineQueries(ctx context.Context) (context.Context, *QueryCounts) {
	counts := new(QueryCounts)
	return zdb.WithDB(ctx, zdb.NewMetricsDB(zdb.MustGetDB(ctx), counts)), counts
}

// DBWithQueryFileCounts tracks named queries without wrapping zdb's loader.
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
