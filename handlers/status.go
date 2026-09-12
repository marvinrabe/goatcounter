package handlers

import (
	"net/http"

	"zgo.at/zdb"
)

// status verifies database connectivity without loading site metadata.
func status(db zdb.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var one int
		if err := db.Get(r.Context(), &one, "select 1"); err != nil {
			http.Error(w, "database unreachable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			w.Write([]byte("OK"))
		}
	}
}
