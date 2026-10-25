package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeProbe is a scriptable Probe; it counts calls so liveness can be proven inert.
type fakeProbe struct {
	name     string
	required bool
	err      error
	calls    int
}

func (f *fakeProbe) Name() string   { return f.name }
func (f *fakeProbe) Required() bool { return f.required }
func (f *fakeProbe) Check(context.Context) error {
	f.calls++
	return f.err
}

func newTestHandler(t *testing.T, probes ...Probe) http.Handler {
	t.Helper()
	return New(Options{
		Probes: probes,
		Logger: slog.New(slog.DiscardHandler),
	})
}

func do(t *testing.T, handler http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestLiveIsHealthyAndNeverTouchesProbes(t *testing.T) {
	probe := &fakeProbe{name: "postgres", required: true, err: errors.New("down")}
	rec := do(t, newTestHandler(t, probe), http.MethodGet, "/health/live")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if probe.calls != 0 {
		t.Errorf("probe called %d times, want 0 for liveness", probe.calls)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
}

func TestReadyReportsHealthyDependencies(t *testing.T) {
	handler := newTestHandler(t,
		&fakeProbe{name: "postgres", required: true},
		&fakeProbe{name: "redis", required: true},
	)
	rec := do(t, handler, http.MethodGet, "/health/ready")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body readyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if len(body.Checks) != 2 {
		t.Fatalf("checks = %d, want 2", len(body.Checks))
	}
	if body.Checks[0].Name != "postgres" || body.Checks[0].Status != "ok" || !body.Checks[0].Required {
		t.Errorf("unexpected first check: %+v", body.Checks[0])
	}
}

func TestReadyFailsWhenRequiredDependencyIsDown(t *testing.T) {
	handler := newTestHandler(t,
		&fakeProbe{name: "postgres", required: true, err: errors.New("connection refused")},
		&fakeProbe{name: "redis", required: true},
	)
	rec := do(t, handler, http.MethodGet, "/health/ready")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	var body readyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.Status != "unavailable" {
		t.Errorf("status = %q, want unavailable", body.Status)
	}
	if body.Checks[0].Status != "error" {
		t.Errorf("failing check status = %q, want error", body.Checks[0].Status)
	}
}

func TestReadyDegradesWhenOptionalDependencyIsDown(t *testing.T) {
	handler := newTestHandler(t,
		&fakeProbe{name: "postgres", required: true},
		&fakeProbe{name: "rabbitmq", required: false, err: errors.New("dial failed")},
	)
	rec := do(t, handler, http.MethodGet, "/health/ready")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 for degraded report", rec.Code)
	}

	var body readyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.Status != "degraded" {
		t.Errorf("status = %q, want degraded", body.Status)
	}
}

func TestReadyWithNoProbesIsHealthy(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodGet, "/health/ready")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"status":"ok","checks":[]}` {
		t.Errorf("body = %s, want empty checks array", got)
	}
}

func TestUnknownPathReturnsJSONNotFound(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodGet, "/nope")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.Error.Code != "not_found" {
		t.Errorf("error code = %q, want %q", body.Error.Code, "not_found")
	}
}
