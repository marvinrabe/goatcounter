package handlers

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
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
