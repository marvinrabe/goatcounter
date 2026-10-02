package handlers

import (
	"net/http"
	"sync"
	"time"

	"github.com/marvinrabe/goatcounter/internal/database"
)

// status verifies database connectivity without loading site metadata.
//
// A successful check is reused for a minute, so frequent health probes don't
// keep the database busy.
func status(db database.DB) http.HandlerFunc {
	var (
		mu     sync.Mutex
		lastOK time.Time
	)
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fresh := time.Since(lastOK) < time.Minute
		mu.Unlock()
		if !fresh {
			var one int
			if err := db.Get(r.Context(), &one, "select 1"); err != nil {
				http.Error(w, "database unreachable", http.StatusServiceUnavailable)
				return
			}
			mu.Lock()
			lastOK = time.Now()
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			w.Write([]byte("OK"))
		}
	}
}
