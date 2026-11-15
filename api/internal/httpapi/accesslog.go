package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// requestIDHeader is how a caller correlates a response with its log line. It is echoed back
// whether it came from the client or was generated here.
const requestIDHeader = "X-Request-ID"

// accessLog writes one line per request.
//
// Two decisions are deliberate:
//
//   - The **route pattern** is logged, never the path or the query. `r.Pattern` is the pattern the
//     mux matched ("GET /v1/places/{coPlace}/reviews"), so the log stays low-cardinality, carries no
//     ids, and — the reason that matters — never records coordinates. Discovery takes a coordinate
//     in the query string and D11 says the coordinate is never logged; an access log that records
//     the URL would leak exactly what the read path refuses to store.
//   - Health probes are logged at debug. They run every few seconds and would otherwise be the
//     loudest thing in the log while telling nobody anything.
func accessLog(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if requestID == "" || len(requestID) > 64 {
			requestID = newRequestID()
		}
		w.Header().Set(requestIDHeader, requestID)

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		// r.Pattern is "GET /v1/places/{coPlace}", the matched pattern including the method. The
		// catch-all mounts "/" and answers 404, so a 404 on "/" is how an unmatched request looks
		// here: the path is the only thing that says what was asked for, and it is worth seeing.
		route := r.Pattern
		if route == "" || (route == "/" && recorder.status == http.StatusNotFound) {
			route = "unmatched " + r.URL.Path
		}

		level := slog.LevelInfo
		switch {
		case strings.Contains(route, "/health/"):
			level = slog.LevelDebug
		case recorder.status >= 500:
			level = slog.LevelWarn
		}

		logger.LogAttrs(r.Context(), level, "request",
			slog.String("method", r.Method),
			slog.String("route", route),
			slog.Int("status", recorder.status),
			slog.Int("bytes", recorder.bytes),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.String("request_id", requestID),
			slog.String("remote_addr", r.RemoteAddr),
		)
	})
}

// statusRecorder captures what the handler answered without changing it.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(body []byte) (int, error) {
	written, err := s.ResponseWriter.Write(body)
	s.bytes += written
	return written, err
}

// Unwrap lets http.ResponseController reach the underlying writer, so a handler that needs to
// flush or hijack still can.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// newRequestID returns 16 hex characters. It is not a secret and not a UUID: it only has to be
// unique enough to find the line.
func newRequestID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		// A random source that fails is not a reason to drop the log line.
		return "0000000000000000"
	}
	return hex.EncodeToString(buffer)
}
