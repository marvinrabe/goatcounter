package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sethvargo/go-limiter"
	"github.com/sethvargo/go-limiter/memorystore"
)

func mustNewMem(tokens uint64, interval time.Duration) limiter.Store {
	s, err := memorystore.New(&memorystore.Config{Tokens: tokens, Interval: interval})
	if err != nil {
		// memorystore.New never returns an error, but just in case.
		panic(err)
	}
	return s
}

type Ratelimits struct {
	Count limiter.Store
}

func NewRatelimits() Ratelimits {
	return Ratelimits{Count: mustNewMem(4, time.Second)}
}

// ClearCount disables the collector limit and releases its store.
// Configure limits before mounting the routes.
func (r *Ratelimits) ClearCount() {
	if r.Count != nil {
		r.Count.Close(context.Background())
		r.Count = nil
	}
}

// SetCount replaces the collector limit, releasing the previous store.
// Configure limits before mounting the routes.
func (r *Ratelimits) SetCount(tokens uint64, interval time.Duration) {
	r.ClearCount()
	r.Count = mustNewMem(tokens, interval)
}

// countMiddleware applies the collector's limits after RealIP has normalized
// the client address. Create the fallback store once per router: allocating a
// memorystore per request would leak its cleanup goroutine.
func (r Ratelimits) countMiddleware(dev bool) func(http.Handler) http.Handler {
	// Localhost and development requests still receive rate-limit headers,
	// but use a limit high enough to be effectively unrestricted. Localhost
	// also includes a local reverse proxy without forwarding headers.
	unlimitedTokens := uint64(1 << 14)
	if dev {
		unlimitedTokens = 1 << 30
	}
	unlimited := mustNewMem(unlimitedTokens, 1)

	return Ratelimit(true, func(req *http.Request) ([]limiter.Store, string) {
		if dev || req.RemoteAddr == "127.0.0.1" {
			return []limiter.Store{unlimited}, ""
		}
		return []limiter.Store{r.Count}, ""
	})
}

func Ratelimit(withUA bool, getStore func(r *http.Request) ([]limiter.Store, string)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stores, msg := getStore(r)
			if len(stores) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			key := r.RemoteAddr
			if withUA {
				// Add in the User-Agent for some endpoints to reduce the
				// problem of multiple people in the same building hitting the
				// limit.
				key += r.UserAgent()
			}

			// Report either the failing ratelimit or the last one, It's assumed
			// that ratelimits are ordered from longest to shortest.
			var (
				tokens, remaining, reset uint64
				ok, active               bool
			)
			for _, store := range stores {
				if store == nil {
					continue
				}
				active = true
				var err error
				tokens, remaining, reset, ok, err = store.Take(r.Context(), key)
				if err != nil {
					// The memorystore only returns an error if Close() was called.
					// But log just to be sure.
					slog.With("module", "ratelimit").ErrorContext(r.Context(), err.Error(), "key", key)
					ok = false
				}
				if !ok {
					break
				}
			}
			if !active {
				next.ServeHTTP(w, r)
				return
			}

			t := time.Unix(0, int64(reset))
			exp := -time.Since(t)
			retryAfter := strconv.FormatFloat(exp.Seconds(), 'f', 0, 64)
			w.Header().Set("X-Rate-Limit-Limit", strconv.FormatUint(tokens, 10))
			w.Header().Set("X-Rate-Limit-Remaining", strconv.FormatUint(remaining, 10))
			w.Header().Set("X-Rate-Limit-Reset", retryAfter)

			if !ok {
				w.Header().Set("Retry-After", retryAfter)
				w.WriteHeader(http.StatusTooManyRequests)

				if msg == "" {
					msg = fmt.Sprintf("rate limited exceeded; try again in %s", exp)
				}
				if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
					fmt.Fprintf(w, `{"error": %q}`, msg)
				} else {
					fmt.Fprintf(w, "%s\n", msg)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
