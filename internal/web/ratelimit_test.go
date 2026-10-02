package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sethvargo/go-limiter"
)

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
