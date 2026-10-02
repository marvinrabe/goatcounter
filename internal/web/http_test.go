package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func init() {
	if err := LoadTemplates(); err != nil {
		panic(err)
	}

	testenv.DefaultHost = "test.example.com"
	slog.SetDefault(slog.New(slog.DiscardHandler))
}

func login(t *testing.T, r *http.Request) {
	t.Helper()
	r.SetBasicAuth("test@example.com", "coconuts")
}
func newTest(method, path string, body io.Reader) (*http.Request, *httptest.ResponseRecorder) {
	r, rr := testenv.NewRequest(method, path, body), httptest.NewRecorder()
	r.Header.Set("User-Agent", "GoatCounter test runner/1.0")
	r.Host = "test"
	return r, rr
}

func TestErrPageStructuredLog(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{AddSource: true})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	r := httptest.NewRequest(http.MethodGet, "https://example.com/dashboard?token=secret", nil)
	r.Header.Set("Content-Type", "text/plain")
	r.Header.Set("User-Agent", "test-agent")
	w := httptest.NewRecorder()

	ErrPage(w, r, fmt.Errorf("load dashboard: %w", io.ErrUnexpectedEOF))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want %d", w.Code, http.StatusInternalServerError)
	}
	if strings.Contains(w.Body.String(), "unexpected EOF") {
		t.Fatalf("response exposed internal error: %q", w.Body.String())
	}
	if strings.Contains(logs.String(), "token=secret") {
		t.Fatalf("log exposed query parameters: %s", logs.String())
	}

	var entry struct {
		Message string         `json:"msg"`
		Module  string         `json:"module"`
		Error   string         `json:"error"`
		Source  map[string]any `json:"source"`
		HTTP    struct {
			Method string `json:"method"`
			Path   string `json:"path"`
			Host   string `json:"host"`
			UA     string `json:"ua"`
		} `json:"http"`
	}
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode log: %v\n%s", err, logs.String())
	}
	if entry.Message != "HTTP request failed" || entry.Module != "http-500" ||
		entry.Error != "load dashboard: unexpected EOF" {
		t.Errorf("unexpected log fields: %#v", entry)
	}
	if entry.HTTP.Method != http.MethodGet || entry.HTTP.Path != "/dashboard" ||
		entry.HTTP.Host != "example.com" || entry.HTTP.UA != "test-agent" {
		t.Errorf("unexpected HTTP fields: %#v", entry.HTTP)
	}
	if entry.Source["file"] == nil || entry.Source["line"] == nil {
		t.Errorf("log has no source location: %#v", entry.Source)
	}
}
