package database

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

func DumpString(ctx context.Context, query string, flags ...bool) string {
	var b strings.Builder
	Dump(ctx, &b, query, flags...)
	return b.String()
}
func Dump(ctx context.Context, w io.Writer, query string, flags ...bool) {
	db := MustGetDB(ctx)
	rows, err := db.queryer().QueryxContext(ctx, query)
	if err != nil {
		fmt.Fprintln(w, err)
		return
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		fmt.Fprintln(w, err)
		return
	}
	data := [][]string{cols}
	width := make([]int, len(cols))
	for i, c := range cols {
		width[i] = len(c)
	}
	for rows.Next() {
		vals, err := rows.SliceScan()
		if err != nil {
			fmt.Fprintln(w, err)
			return
		}
		line := make([]string, len(vals))
		for i, v := range vals {
			switch v := v.(type) {
			case nil:
				line[i] = "NULL"
			case []byte:
				line[i] = string(v)
			case time.Time:
				line[i] = v.Format("2006-01-02 15:04:05")
			default:
				line[i] = fmt.Sprint(v)
			}
			width[i] = max(width[i], len(line[i]))
		}
		data = append(data, line)
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintln(w, err)
		return
	}
	vertical := len(flags) > 0 && flags[0]
	if vertical {
		size := 0
		for _, c := range cols {
			size = max(size, len(c))
		}
		for _, line := range data[1:] {
			for i, c := range cols {
				fmt.Fprintf(w, "%-*s  %s\n", size, c, line[i])
			}
		}
		return
	}
	for _, line := range data {
		for i, s := range line {
			if i > 0 {
				fmt.Fprint(w, "  ")
			}
			if i == len(line)-1 {
				fmt.Fprint(w, s)
			} else {
				fmt.Fprintf(w, "%-*s", width[i], s)
			}
		}
		fmt.Fprintln(w)
	}
}
