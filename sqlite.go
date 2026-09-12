package goatcounter

import (
	"context"
	"fmt"
	"math"

	"github.com/mattn/go-sqlite3"
)

// SQLiteHook registers GoatCounter's custom SQLite functions on a connection.
var SQLiteHook = func(c *sqlite3.SQLiteConn) error {
	return c.RegisterFunc("percent_diff", func(start, final int) float64 {
		if start == 0 {
			return math.Inf(0)
		}
		return (float64(final - start)) / float64(start) * 100.0
	}, true)
}

func Interval(ctx context.Context, days int) string {
	return fmt.Sprintf(" datetime(datetime(), '-%d days') ", days)
}
