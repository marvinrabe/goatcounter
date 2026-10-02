package web

import (
	"fmt"
	"net/http"

	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/enrich"
	"github.com/monoculum/formam/v3"
)

// count stores a pageview or event sent by count.js with sendBeacon() or
// fetch(). Parameters are in the query string; the response is always empty.
//
// Errors are reported in the X-Goatcounter header, for debugging.
func (s *server) count(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	fail := func(msg string, args ...any) error {
		w.Header().Set("X-Goatcounter", fmt.Sprintf(msg, args...))
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}

	// Bots and prefetches aren't stored, and the client doesn't need to know.
	if enrich.IsBot(r) {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	event := analytics.EventFromRequest(r)
	q := r.URL.Query()
	if q.Get("s") == "" {
		q.Del("s") // An unknown width is 0, not an error.
	}
	err := formam.NewDecoder(&formam.DecoderOptions{TagName: "json", IgnoreUnknownKeys: true}).
		Decode(q, &event)
	if err != nil {
		return fail("error decoding parameters: %s", err)
	}
	if event.Bot > 0 && event.Bot < 150 {
		return fail("wrong value: b=%d", event.Bot)
	}
	if event.Bot > 0 {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	if err := event.Validate(); err != nil {
		return fail("not valid: %s", err)
	}

	if err := s.store.Collect(r.Context(), siteFrom(r.Context()), event); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
