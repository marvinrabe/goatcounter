package database

import (
	"bytes"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"reflect"
	"strings"
	"text/template"
	"uuid"
)

// SQL is a trusted SQL fragment, never a value supplied by a client.
type SQL string

var TemplateFuncMap template.FuncMap

func Template(query string, params ...any) ([]byte, error) {
	data := map[string]any{}
	for _, p := range params {
		if m, ok := p.(map[string]any); ok {
			maps.Copy(data, m)
		}
	}
	funcs := template.FuncMap{
		"auto_increment": func(...bool) string { return "integer primary key autoincrement" },
		"blob":           func() string { return "blob" },
	}
	maps.Copy(funcs, TemplateFuncMap)
	t, err := template.New("sql").Funcs(funcs).Parse(query)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	err = t.Execute(&b, data)
	return b.Bytes(), err
}

func Load(db DB, name string) (string, bool, error) {
	if db.files == nil {
		return "", false, fmt.Errorf("database: no query files configured")
	}
	name = strings.TrimPrefix(name, "load:")
	names := []string{name}
	if !strings.HasSuffix(name, ".sql") && !strings.HasSuffix(name, ".gotxt") {
		names = []string{name + ".sql", name + ".gotxt"}
	}
	for _, name := range names {
		for _, prefix := range []string{"db/query/", "query/"} {
			b, err := fs.ReadFile(db.files, prefix+name)
			if err == nil {
				return string(b), strings.Contains(string(b), "{{"), nil
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return "", false, err
			}
		}
	}
	return "", false, fmt.Errorf("database: query %q: %w", name, fs.ErrNotExist)
}

func (db *Database) prepare(query string, params ...any) (string, []any, error) {
	data := map[string]any{}
	positional := []any{}
	for _, p := range params {
		if m, ok := p.(map[string]any); ok {
			maps.Copy(data, m)
		} else {
			positional = append(positional, p)
		}
	}
	if strings.HasPrefix(query, "load:") {
		loaded, _, err := Load(db, query)
		if err != nil {
			return "", nil, err
		}
		query = loaded
	}
	if strings.Contains(query, "{{") {
		b, err := Template(query, data)
		if err != nil {
			return "", nil, err
		}
		query = string(b)
	}
	var out strings.Builder
	var args []any
	index := 0
	var bind func(string, int) error
	var value func(any, int) error
	value = func(v any, depth int) error {
		if literal, ok := v.(SQL); ok {
			return bind(string(literal), depth+1)
		}
		if id, ok := v.(uuid.UUID); ok {
			v = id[:]
		}
		// Scanner/Valuer types (UUIDs and comma-separated slices) are scalar values.
		if _, ok := v.(driver.Valuer); !ok && v != nil {
			rv := reflect.ValueOf(v)
			if rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() != reflect.Uint8 {
				if rv.Len() == 0 {
					out.WriteString("NULL")
					return nil
				}
				for i := 0; i < rv.Len(); i++ {
					if i > 0 {
						out.WriteByte(',')
					}
					if err := value(rv.Index(i).Interface(), depth); err != nil {
						return err
					}
				}
				return nil
			}
		}
		out.WriteByte('?')
		args = append(args, v)
		return nil
	}
	bind = func(q string, depth int) error {
		if depth > 16 {
			return fmt.Errorf("database: recursive SQL fragment")
		}
		for i := 0; i < len(q); {
			// Parameters inside SQL strings, quoted identifiers, and comments stay literal.
			start := i
			if q[i] == '\'' || q[i] == '"' || q[i] == '`' {
				quote := q[i]
				i++
				for i < len(q) {
					if q[i] == quote {
						i++
						if i < len(q) && q[i] == quote {
							i++
							continue
						}
						break
					}
					i++
				}
				out.WriteString(q[start:i])
				continue
			}
			if strings.HasPrefix(q[i:], "--") {
				j := strings.IndexByte(q[i:], '\n')
				if j < 0 {
					j = len(q) - i
				}
				out.WriteString(q[i : i+j])
				i += j
				continue
			}
			if strings.HasPrefix(q[i:], "/*") {
				j := strings.Index(q[i+2:], "*/")
				if j < 0 {
					return fmt.Errorf("database: unterminated SQL comment")
				}
				i += j + 4
				out.WriteString(q[start:i])
				continue
			}
			if q[i] == ':' && i+1 < len(q) && ident(q[i+1]) {
				i += 2
				for i < len(q) && ident(q[i]) {
					i++
				}
				key := q[start+1 : i]
				v, ok := data[key]
				if !ok {
					return fmt.Errorf("database: missing parameter %q", key)
				}
				if err := value(v, depth); err != nil {
					return err
				}
				continue
			}
			if q[i] == '?' {
				if index >= len(positional) {
					return fmt.Errorf("database: missing positional parameter")
				}
				if err := value(positional[index], depth); err != nil {
					return err
				}
				index++
				i++
				continue
			}
			out.WriteByte(q[i])
			i++
		}
		return nil
	}
	if err := bind(query, 0); err != nil {
		return "", nil, err
	}
	if index != len(positional) {
		return "", nil, fmt.Errorf("database: unused positional parameters")
	}
	return out.String(), args, nil
}
func ident(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}
