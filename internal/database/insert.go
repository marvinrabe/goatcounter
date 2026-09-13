package database

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

type Tabler interface{ Table() string }
type Defaulter interface{ Defaults(context.Context) }
type Validator interface{ Validate(context.Context) error }

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func Insert(ctx context.Context, row Tabler) error {
	if v, ok := row.(Defaulter); ok {
		v.Defaults(ctx)
	}
	if v, ok := row.(Validator); ok {
		if err := v.Validate(ctx); err != nil {
			return err
		}
	}
	v := reflect.ValueOf(row)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("database.Insert: expected a non-nil pointer")
	}
	v = v.Elem()
	typ := v.Type()
	var cols []string
	var args []any
	var id any
	var idcol string
	for i := 0; i < v.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		tags := strings.Split(field.Tag.Get("db"), ",")
		if tags[0] == "-" || tags[0] == "" || slices.Contains(tags, "noinsert") {
			continue
		}
		if slices.Contains(tags, "id") {
			if !v.Field(i).IsZero() {
				return fmt.Errorf("database.Insert: ID must be zero")
			}
			id, idcol = v.Field(i).Addr().Interface(), tags[0]
			continue
		}
		cols = append(cols, quote(tags[0]))
		args = append(args, v.Field(i).Interface())
	}
	query := "insert into " + quote(row.Table()) + " (" + strings.Join(cols, ",") + ") values (" + strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",") + ")"
	if id != nil {
		return Get(ctx, id, query+" returning "+quote(idcol), args...)
	}
	return Exec(ctx, query, args...)
}

type BulkInsert struct {
	ctx      context.Context
	table    string
	cols     []string
	conflict string
	rows     [][]any
	err      error
}

func NewBulkInsert(ctx context.Context, table string, columns []string) (BulkInsert, error) {
	if len(columns) == 0 {
		return BulkInsert{}, fmt.Errorf("database: empty insert columns")
	}
	return BulkInsert{ctx: ctx, table: table, cols: slices.Clone(columns)}, nil
}
func (b *BulkInsert) OnConflict(s string) { b.conflict = s }
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
	query := "insert into " + quote(b.table) + " (" + strings.Join(cols, ",") + ") values " + strings.TrimSuffix(strings.Repeat(row+",", len(b.rows)), ",") + " " + b.conflict
	b.err = Exec(b.ctx, query, args...)
	b.rows = nil
}
func (b *BulkInsert) Finish() error { b.flush(); return b.err }
