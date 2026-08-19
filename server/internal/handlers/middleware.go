package handlers

import "net/http"

// WithLocalCORS exists purely for local debugging convenience (e.g. hitting
// this API directly from a browser tab to sanity-check a response). The
// Next.js BFF itself talks to this service server-to-server and never hits
// this CORS restriction at all.
func WithLocalCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent) //204
			return
		}
		next.ServeHTTP(w, r)
	})
}
