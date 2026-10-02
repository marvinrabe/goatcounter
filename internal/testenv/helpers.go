package testenv

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func Diff(got, want string) string {
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
func MustUnmarshal(b []byte, v any) {
	if err := json.Unmarshal(b, v); err != nil {
		panic(err)
	}
}
