package database

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// identifier reports if s is a plain lowercase SQL name. Table and column
// names can't be bound as parameters, so only these are accepted rather than
// escaping arbitrary strings.
func identifier(s string) bool {
	for i, c := range s {
		if !(c == '_' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return s != ""
}

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
	for _, name := range append([]string{table}, columns...) {
		if !identifier(name) {
			return BulkInsert{}, fmt.Errorf("database: invalid table or column name %q", name)
		}
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
	row := "(" + strings.TrimSuffix(strings.Repeat("?,", len(b.cols)), ",") + ")"
	args := make([]any, 0, len(b.rows)*len(b.cols))
	for _, v := range b.rows {
		args = append(args, v...)
	}
	query := "insert into " + b.table + " (" + strings.Join(b.cols, ",") + ") values " + strings.TrimSuffix(strings.Repeat(row+",", len(b.rows)), ",")
	b.err = b.db.Exec(b.ctx, query, args...)
	b.rows = nil
}
func (b *BulkInsert) Finish() error { b.flush(); return b.err }
