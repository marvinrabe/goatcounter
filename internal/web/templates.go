package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/marvinrabe/goatcounter/internal/analytics"
)

// Keep the last successfully parsed set available while development reloads.
var pageTemplates atomic.Pointer[template.Template]

// LoadTemplates parses the embedded templates.
func LoadTemplates() error { return parseTemplates(sub(templateFiles, "templates")) }

// parseTemplates parses the templates in files before publishing the new set.
func parseTemplates(files fs.FS) error {
	t, err := template.New("").Funcs(template.FuncMap{
		"json": func(v any) (string, error) {
			b, err := json.Marshal(v)
			return string(b), err
		},
		"nformat": formatNumber,
		"tformat": func(ctx context.Context, t time.Time, format string) string {
			if format == "" {
				format = "2006-01-02"
			}
			return t.In(analytics.Config(ctx).Timezone.Loc()).Format(format)
		},
		"path_id": func(p string) string {
			p = strings.ReplaceAll(strings.TrimLeft(p, "/"), "/", "-")
			if p == "" {
				return "dashboard"
			}
			return p
		},
		"change": metricChange,
		"metric": func(key, label, value string, c change) metric { return metric{key, label, value, c} },
		"list":   func(s ...string) []string { return s },
	}).ParseFS(files, "*.gohtml")
	if err != nil {
		return err
	}
	pageTemplates.Store(t)
	return nil
}

func renderTemplate(name string, data any) (string, error) {
	t := pageTemplates.Load()
	if t == nil {
		return "", fmt.Errorf("templates are not loaded")
	}
	var b strings.Builder
	err := t.ExecuteTemplate(&b, name, data)
	return b.String(), err
}

// Buffer the page so a render error cannot send a partial successful response.
func renderHTML(w http.ResponseWriter, name string, data any) error {
	body, err := renderTemplate(name, data)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err = w.Write([]byte(body))
	return err
}

func formatNumber(n int) string {
	s := strconv.Itoa(n)
	start := 0
	if n < 0 {
		start = 1
	}
	for i := len(s) - 3; i > start; i -= 3 {
		s = s[:i] + "\u202f" + s[i:]
	}
	return s
}

// metric is one of the totals on the dashboard.
type metric struct {
	Key, Label, Value string
	Change            change
}

type change struct {
	Show, Up, Good bool
	Text           string
}

// metricChange describes the change of a metric from the previous period.
// lowerIsBetter is for the bounce rate.
func metricChange(cur, prev any, lowerIsBetter bool) change {
	f := func(v any) float64 {
		switch v := v.(type) {
		case int:
			return float64(v)
		case float64:
			return v
		}
		return 0
	}
	pct, ok := analytics.Change(f(cur), f(prev))
	if !ok {
		return change{}
	}
	c := change{Show: true, Up: pct >= 0, Text: fmt.Sprintf("%.0f%%", math.Abs(pct))}
	c.Good = c.Up != lowerIsBetter
	if c.Text == "0%" {
		c.Good, c.Up = true, true
	}
	return c
}
