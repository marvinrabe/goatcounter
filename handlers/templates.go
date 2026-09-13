package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/marvinrabe/goatcounter"
)

// Keep the last successfully parsed set available while development reloads.
var pageTemplates atomic.Pointer[template.Template]

// LoadTemplates parses the application's templates before publishing the new set.
func LoadTemplates(files fs.FS) error {
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
			return t.In(goatcounter.Config(ctx).Timezone.Loc()).Format(format)
		},
		"path_id": func(p string) string {
			p = strings.ReplaceAll(strings.TrimLeft(p, "/"), "/", "-")
			if p == "" {
				return "dashboard"
			}
			return p
		},
		"horizontal_chart":       horizontalChart,
		"horizontal_chart_pages": horizontalChartPages,
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
