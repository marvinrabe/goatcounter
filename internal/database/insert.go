package database

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

type BulkInsert struct {
	ctx   context.Context
	db    DB
	table string
	cols  []string
	rows  [][]any
	err   error
}

func NewBulkInsert(ctx context.Context, db DB, table string, columns []string) (BulkInsert, error) {
	if len(columns) == 0 {
		return BulkInsert{}, fmt.Errorf("database: empty insert columns")
	}
	return BulkInsert{ctx: ctx, db: db, table: table, cols: slices.Clone(columns)}, nil
}
func (b *BulkInsert) Values(v ...any) {
	if b.err != nil {
		return
	}
	if len(v) != len(b.cols) {
		b.err = fmt.Errorf("database: insert has %d values for %d columns", len(v), len(b.cols))
		return
	}
	b.rows = append(b.rows, slices.Clone(v))
	if len(b.rows)*len(b.cols) >= 900 {
		b.flush()
	}
}
func (b *BulkInsert) flush() {
	if b.err != nil || len(b.rows) == 0 {
		return
	}
	cols := make([]string, len(b.cols))
	for i, c := range b.cols {
		cols[i] = quote(c)
	}
	row := "(" + strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",") + ")"
	args := make([]any, 0, len(b.rows)*len(cols))
	for _, v := range b.rows {
		args = append(args, v...)
	}
	query := "insert into " + quote(b.table) + " (" + strings.Join(cols, ",") + ") values " + strings.TrimSuffix(strings.Repeat(row+",", len(b.rows)), ",")
	b.err = b.db.Exec(b.ctx, query, args...)
	b.rows = nil
}
func (b *BulkInsert) Finish() error { b.flush(); return b.err }
