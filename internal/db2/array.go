package db2

import (
	"context"
	"strconv"
	"strings"

	"zgo.at/zdb"
)

func In(ctx context.Context) zdb.SQL {
	return "in"
}

func NotIn(ctx context.Context, col string) zdb.SQL {
	return zdb.SQL("not in " + col)
}

func Array[T ~int8 | ~int16 | ~int32 | ~int64](ctx context.Context, p []T) any {
	if zdb.SQLDialect(ctx) == zdb.DialectSQLite {
		return p
	}

	if len(p) == 0 {
		return `{}`
	}

	var b strings.Builder
	b.Grow(len(p) * 3)
	b.WriteByte('{')
	for i, pp := range p {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(int64(pp), 10))
	}
	b.WriteByte('}')
	return b.String()
}
