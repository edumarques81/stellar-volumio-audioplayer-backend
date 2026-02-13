package main

import "net/http"

// corsMiddleware wraps an http.Handler and sets CORS headers on ALL responses,
// including error responses. This prevents CORB (Cross-Origin Read Blocking) in
// browsers when the frontend (port 8080) makes cross-origin requests to the
// backend (port 3000).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
