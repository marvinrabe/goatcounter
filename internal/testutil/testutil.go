// Package testutil contains small fixture and comparison helpers for tests.
package testutil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type DiffOpt int

const (
	DiffNormalizeWhitespace DiffOpt = iota + 1
	DiffJSON
)

func Diff(got, want string, opts ...DiffOpt) string {
	for _, opt := range opts {
		switch opt {
		case DiffNormalizeWhitespace:
			normalize := func(s string) string {
				lines := strings.Split(s, "\n")
				for i := range lines {
					lines[i] = strings.TrimSpace(lines[i])
				}
				return strings.Join(lines, "\n")
			}
			got, want = normalize(got), normalize(want)
		case DiffJSON:
			normalize := func(s string) (string, error) {
				var value any
				d := json.NewDecoder(strings.NewReader(s))
				d.UseNumber()
				if err := d.Decode(&value); err != nil {
					return "", err
				}
				b, err := json.MarshalIndent(value, "", "  ")
				return string(b), err
			}
			var err error
			got, err = normalize(got)
			if err != nil {
				return fmt.Sprintf("invalid actual JSON: %v", err)
			}
			want, err = normalize(want)
			if err != nil {
				return fmt.Sprintf("invalid expected JSON: %v", err)
			}
		}
	}
	got, want = strings.TrimSpace(got), strings.TrimSpace(want)
	if got == want {
		return ""
	}
	return fmt.Sprintf("\ngot:\n%s\nwant:\n%s", got, want)
}
func NormalizeIndent(s string) string {
	lines := strings.Split(strings.Trim(s, "\n"), "\n")
	prefix := ""
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			prefix = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			break
		}
	}
	for i := range lines {
		lines[i] = strings.TrimPrefix(lines[i], prefix)
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}
func ErrorContains(err error, want string) bool {
	if want == "" {
		return err == nil
	}
	return err != nil && strings.Contains(err.Error(), want)
}
func Code(t testing.TB, r *httptest.ResponseRecorder, want int) {
	t.Helper()
	if r.Code != want {
		t.Errorf("status=%d, want %d; body: %s", r.Code, want, r.Body.String())
	}
}

var DefaultHost = "example.com"

func NewRequest(method, path string, body io.Reader) *http.Request {
	if strings.HasPrefix(path, "/") {
		path = "http://" + DefaultHost + path
	}
	return httptest.NewRequest(method, path, body)
}
func MustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func MustMarshalString(v any) string { return string(MustMarshal(v)) }
func MustMarshalIndent(v any, prefix, indent string) []byte {
	b, err := json.MarshalIndent(v, prefix, indent)
	if err != nil {
		panic(err)
	}
	return b
}
func MustUnmarshal(b []byte, v any) {
	if err := json.Unmarshal(b, v); err != nil {
		panic(err)
	}
}
func ModuleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if dir == parent {
			panic("cannot find module root")
		}
		dir = parent
	}
}
