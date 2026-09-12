package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sethvargo/go-limiter"
)

func TestConfigureCountLimit(t *testing.T) {
	limits := NewRatelimits()
	t.Cleanup(limits.ClearCount)
	previous := limits.Count
	limits.SetCount(2, time.Minute)
	if _, _, _, _, err := previous.Take(context.Background(), "visitor"); !errors.Is(err, limiter.ErrStopped) {
		t.Errorf("previous store was not closed: %v", err)
	}
	if tokens, _, _, ok, err := limits.Count.Take(context.Background(), "visitor"); err != nil || !ok || tokens != 2 {
		t.Errorf("new limit: tokens=%d ok=%t error=%v", tokens, ok, err)
	}
	previous = limits.Count
	limits.ClearCount()
	if limits.Count != nil {
		t.Error("collector limit was not disabled")
	}
	if _, _, _, _, err := previous.Take(context.Background(), "visitor"); !errors.Is(err, limiter.ErrStopped) {
		t.Errorf("disabled store was not closed: %v", err)
	}
}

func TestRatelimit(t *testing.T) {
	for _, disabled := range []bool{true, false} {
		var store limiter.Store
		if !disabled {
			store = mustNewMem(1, time.Hour)
			t.Cleanup(func() { store.Close(context.Background()) })
		}
		h := Ratelimit(false, func(*http.Request) ([]limiter.Store, string) {
			return []limiter.Store{nil, store, nil}, ""
		})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		for i := range 2 {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/count", nil))
			code := http.StatusNoContent
			if !disabled && i == 1 {
				code = http.StatusTooManyRequests
			}
			if w.Code != code {
				t.Errorf("disabled=%t request=%d: status = %d; want %d", disabled, i, w.Code, code)
			}
			if disabled && w.Header().Get("X-Rate-Limit-Limit") != "" {
				t.Error("disabled rate limit should not set limit headers")
			}
		}
	}
}
