// Package httpx provides the response helpers used by the application.
package httpx

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type statusError struct {
	status int
	cause  error
}

func (e statusError) Error() string          { return e.cause.Error() }
func (e statusError) Code() int              { return e.status }
func Error(status int, message string) error { return statusError{status, errors.New(message)} }
func Errorf(status int, format string, args ...any) error {
	return statusError{status, fmt.Errorf(format, args...)}
}
func Code(err error) int {
	var e interface{ Code() int }
	if errors.As(err, &e) {
		return e.Code()
	}
	return 0
}
func UserError(err error) (int, error) {
	code := Code(err)
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

var ErrPage = func(w http.ResponseWriter, r *http.Request, err error) {
	code, userErr := UserError(err)
	http.Error(w, userErr.Error(), code)
}

func Wrap(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			ErrPage(w, r, err)
		}
	}
}
func Bytes(w http.ResponseWriter, b []byte) error { _, err := w.Write(b); return err }
func JSON(w http.ResponseWriter, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return Bytes(w, b)
}
func IsSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (e statusError) Unwrap() error { return e.cause }
