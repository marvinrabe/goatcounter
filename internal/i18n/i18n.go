// Package i18n renders translatable messages with tag and variable markup.
package i18n

import (
	"context"
	"fmt"
	"html"
	"html/template"
	"reflect"
	"strconv"
	"strings"
	"time"

	"zgo.at/ztpl/tplfunc"
)

type (
	// P is a shortcut for T() map parameters.
	P map[string]any

	// Plural marks a number as a plural form selector.
	Plural int

	// Tagger is a HTML tag wrapper for %[tag text] markup.
	Tagger interface {
		Open() string
		Close() string
		Text() string
	}
)

// N returns a plural of n.
func N(n int) Plural { return Plural(n) }

type tag struct {
	tag, content, innerHTML string
}

func (t tag) Open() string {
	if t.content == "" {
		return "<" + t.tag + ">"
	}
	return "<" + t.tag + " " + t.content + ">"
}
func (t tag) Close() string  { return "</" + t.tag + ">" }
func (t tag) Text() string   { return t.innerHTML }
func (t tag) String() string { return t.Open() + html.EscapeString(t.Text()) + t.Close() }

// Tag creates a new tag.
func Tag(tagName, content string, innerHTML ...string) tag {
	return tag{tag: tagName, content: content, innerHTML: strings.Join(innerHTML, "")}
}

// T returns the English message for this ID.
func T(ctx context.Context, id string, data ...any) string {
	def := id
	if p := strings.Index(id, "|"); p > -1 {
		def = id[p+1:]
	}

	var (
		params = make(P)
		oneVar bool
	)
	for _, d := range data {
		switch p := d.(type) {
		case Plural:
			params["n"] = int(p)
		case P:
			params = p
		case map[string]any:
			params = p
		case map[string]string:
			params = make(P, len(p))
			for k, v := range p {
				params[k] = v
			}
		default:
			oneVar = true
			params = P{"": d}
		}
	}

	return display(def, params, oneVar)
}

// Thtml is like T, but returns template.HTML so the markup in the message (and
// in any %[tag] parameters) isn't escaped again by html/template.
func Thtml(ctx context.Context, id string, data ...any) template.HTML {
	return template.HTML(T(ctx, id, data...))
}

func indexPairs(str, open, close string) [][]int {
	var pairs [][]int
	for i := 0; ; {
		s := strings.Index(str[i:], open)
		if s < 0 {
			break
		}
		s += i
		e := strings.Index(str[s+len(open):], close)
		if e < 0 {
			break
		}
		e += s + len(open)
		pairs = append(pairs, []int{s, e})
		i = e + len(close)
	}
	return pairs
}

func display(str string, params P, oneVar bool) string {
	// Replace back to front: indexPairs() indexes the string as it is now, and
	// a replacement that isn't the same length as what it replaces would
	// invalidate the indexes of everything after it.
	pairs := indexPairs(str, "%[", "]")
	for i := len(pairs) - 1; i >= 0; i-- {
		p := pairs[i]
		start, end := p[0], p[1]
		text := str[start+2 : end]
		varname := ""
		if len(text) > 0 && text[0] == '%' {
			varname, text, _ = strings.Cut(text, " ")
			varname = varname[1:]
		}

		key := varname
		if oneVar {
			key = ""
		}
		value, ok := params[key]
		if !ok {
			str = str[:start] + "!(no value for tag " + varname + ")" + str[end+1:]
			continue
		}
		t, ok := value.(Tagger)
		if !ok {
			str = str[:start] + "!(value for " + varname + " is not a Tagger)" + str[end+1:]
			continue
		}

		tt := t.Text()
		if tt == "" {
			tt = html.EscapeString(text)
		}
		str = str[:start] + t.Open() + tt + t.Close() + str[end+1:]
	}

	pairs = indexPairs(str, "%(", ")")
	for i := len(pairs) - 1; i >= 0; i-- {
		p := pairs[i]
		start, end := p[0], p[1]
		varname := str[start+2 : end]

		key := varname
		if oneVar {
			key = ""
		}
		val, ok := params[key]
		if !ok {
			str = str[:start] + "!(no value for variable " + varname + ")" + str[end+1:]
			continue
		}

		v := l10n(val)
		if _, isTagger := val.(Tagger); !isTagger {
			if s, ok := val.(string); ok {
				v = html.EscapeString(s)
			}
		}

		str = str[:start] + v + str[end+1:]
	}
	return str
}

func l10n(v any) string {
	switch vv := v.(type) {
	case string:
		return vv
	case time.Time:
		return vv.Format("2006-01-02 15:04")
	case Plural:
		return strconv.Itoa(int(vv))
	case int:
		return strconv.Itoa(vv)
	case fmt.Stringer:
		return vv.String()
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(rv.Float(), 'f', -1, 64)
	case reflect.Bool:
		return strconv.FormatBool(rv.Bool())
	case reflect.String:
		return rv.String()
	}
	return ""
}

func init() {
	tplfunc.Add("t", Thtml)
	tplfunc.Add("tag", Tag)
	tplfunc.Add("plural", N)
}
