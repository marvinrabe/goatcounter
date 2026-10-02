package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/marvinrabe/goatcounter/internal/enrich"
)

type statusError struct {
	status int
	cause  error
}

func (e statusError) Error() string { return e.cause.Error() }
func (e statusError) Unwrap() error { return e.cause }
func (e statusError) Code() int     { return e.status }

func httpError(status int, message string) error {
	return statusError{status, errors.New(message)}
}
func httpErrorf(status int, format string, args ...any) error {
	return statusError{status, fmt.Errorf(format, args...)}
}

// userError returns the status code for err and an error that is safe to show
// to the client.
func userError(err error) (int, error) {
	code := 0
	var e interface{ Code() int }
	if errors.As(err, &e) {
		code = e.Code()
	}
	if code == 0 {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			code = 404
		case errors.Is(err, context.DeadlineExceeded):
			code = 504
		default:
			code = 500
		}
	}
	switch {
	case code == 404:
		return code, errors.New("not found")
	case code == 504:
		return code, errors.New("server timed out loading data")
	case code >= 500:
		return code, errors.New("an unexpected error occurred; please try again")
	}
	return code, err
}

func wrap(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			ErrPage(w, r, err)
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, err = w.Write(b)
	return err
}

func isSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get(enrich.ProtoHeader), "https")
}

// ErrPage logs internal errors and renders a safe response for the client.
func ErrPage(w http.ResponseWriter, r *http.Request, reported error) {
	if reported == nil {
		return
	}
	hasStatus := true
	if ww, ok := w.(statusWriter); !ok || ww.Status() == 0 {
		hasStatus = false
	}

	code, userErr := userError(reported)
	if code >= 500 {
		slog.With("module", "http-500").ErrorContext(r.Context(), "HTTP request failed",
			"error", reported, requestAttrs(r))
	}

	ct := strings.ToLower(r.Header.Get("Content-Type"))
	ctresp := strings.ToLower(w.Header().Get("Content-Type"))
	switch {
	case strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ctresp, "application/json"):
		if !hasStatus {
			w.WriteHeader(code)
		}

		j, _ := json.Marshal(map[string]string{"error": userErr.Error()})
		w.Write(j)

	case strings.HasPrefix(ct, "text/plain") || strings.HasPrefix(ctresp, "text/plain"):
		if !hasStatus {
			w.WriteHeader(code)
		}
		fmt.Fprintf(w, "Error %d: %s", code, userErr)

	default:
		if !hasStatus {
			w.WriteHeader(code)
		}

		t := pageTemplates.Load()
		if t == nil || t.Lookup("error.gohtml") == nil {
			fmt.Fprintf(w, "<pre>Error %d: %s</pre>", code, template.HTMLEscapeString(userErr.Error()))
			return
		}

		styleURL, _ := assetURL("assets/css/backend.css")
		err := t.ExecuteTemplate(w, "error.gohtml", struct {
			Code     int
			Error    error
			Path     string
			StyleURL string
		}{code, userErr, r.URL.Path, styleURL})
		if err != nil {
			slog.ErrorContext(r.Context(), "render error page", "error", err, requestAttrs(r))
		}
	}
}

// Keep request details alongside errors without logging request bodies or query
// parameters, which may contain credentials or other sensitive values.
func requestAttrs(r *http.Request) slog.Attr {
	return slog.Group("http", "method", r.Method, "path", r.URL.Path,
		"host", r.Host, "ua", r.UserAgent())
}
