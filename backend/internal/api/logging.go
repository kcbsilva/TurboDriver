package api

import (
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// JSONLogger emits structured logs with request id, status, and latency.
func JSONLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)

		reqID := middleware.GetReqID(r.Context())
		role := ""
		if id, ok := identityFromContext(r.Context()); ok {
			role = string(id.Role)
		}
		log.Printf(`{"ts":"%s","request_id":"%s","method":"%s","path":"%s","status":%d,"latency_ms":%.3f,"role":"%s"}`,
			time.Now().UTC().Format(time.RFC3339Nano),
			reqID,
			r.Method,
			r.URL.Path,
			rec.status,
			float64(time.Since(start).Microseconds())/1000,
			role,
		)
	})
}
