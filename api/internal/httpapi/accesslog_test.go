package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func logRouter(buffer *bytes.Buffer) http.Handler {
	logger := slog.New(slog.NewTextHandler(buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return New(Options{
		TokenParser: authedParser("user-1"),
		Authorizer:  adminScope,
		Logger:      logger,
	})
}

// The reason this exists: discovery takes a coordinate in the query string, and D11 says the
// coordinate is never logged. An access log that recorded the URL would leak precisely what the
// read path refuses to store.
func TestTheAccessLogRecordsTheRouteAndNeverTheQuery(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// A mux of its own, so the pattern is set by the real router and the middleware is what is
	// under test. Through New() the discovery route is absent unless a service is wired.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/places", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	router := accessLog(mux, logger)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/v1/places?near=40.4168,-3.7038&radius=2000", nil))

	line := buffer.String()
	if !strings.Contains(line, "route=\"GET /v1/places\"") {
		t.Fatalf("log = %q, want the route pattern", line)
	}
	for _, forbidden := range []string{"40.4168", "-3.7038", "radius=2000", "near="} {
		if strings.Contains(line, forbidden) {
			t.Fatalf("log leaked the query (%q): %q", forbidden, line)
		}
	}
}

func TestTheAccessLogRecordsTheStatusAndTheDuration(t *testing.T) {
	var buffer bytes.Buffer
	router := logRouter(&buffer)

	router.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/v1/places/nope", nil))

	line := buffer.String()
	if !strings.Contains(line, "status=404") {
		t.Fatalf("log = %q, want status=404", line)
	}
	if !strings.Contains(line, "duration_ms=") {
		t.Fatalf("log = %q, want a duration", line)
	}
}

// A caller that already has an id keeps it, so a trace can start before the request arrives.
func TestTheAccessLogEchoesAClientRequestID(t *testing.T) {
	var buffer bytes.Buffer
	router := logRouter(&buffer)

	request := httptest.NewRequest(http.MethodGet, "/v1/places", nil)
	request.Header.Set(requestIDHeader, "trace-from-the-edge")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)

	if got := rec.Header().Get(requestIDHeader); got != "trace-from-the-edge" {
		t.Fatalf("response header = %q, want the caller's id", got)
	}
	if !strings.Contains(buffer.String(), "request_id=trace-from-the-edge") {
		t.Fatalf("log = %q", buffer.String())
	}
}

func TestTheAccessLogGeneratesARequestIDWhenThereIsNone(t *testing.T) {
	var buffer bytes.Buffer
	router := logRouter(&buffer)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/places", nil))

	if got := rec.Header().Get(requestIDHeader); len(got) != 16 {
		t.Fatalf("generated id = %q, want 16 hex characters", got)
	}
}

// An unmatched request is the one worth seeing, and the pattern is empty there: the path is the
// only thing that says what was asked for.
func TestTheAccessLogKeepsThePathOfAnUnmatchedRequest(t *testing.T) {
	var buffer bytes.Buffer
	router := logRouter(&buffer)

	router.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/v1/does-not-exist?near=1,2", nil))

	line := buffer.String()
	if !strings.Contains(line, "route=\"unmatched /v1/does-not-exist\"") {
		t.Fatalf("log = %q, want the unmatched path", line)
	}
	if strings.Contains(line, "near=1,2") {
		t.Fatalf("log leaked the query of an unmatched request: %q", line)
	}
}

// Health probes run every few seconds: at info they would be the loudest thing in the log.
func TestHealthProbesAreLoggedAtDebug(t *testing.T) {
	var buffer bytes.Buffer
	router := logRouter(&buffer)

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health/live", nil))

	if !strings.Contains(buffer.String(), "level=DEBUG") {
		t.Fatalf("log = %q, want debug for a probe", buffer.String())
	}
}

// A server error is louder than a normal request: it is the line an operator greps for.
func TestAServerErrorIsLoggedAtWarn(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handler := accessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusInternalServerError, "internal_error", "boom")
	}), logger)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/boom", nil))

	if !strings.Contains(buffer.String(), "level=WARN") || !strings.Contains(buffer.String(), "status=500") {
		t.Fatalf("log = %q, want warn with status 500", buffer.String())
	}
}
