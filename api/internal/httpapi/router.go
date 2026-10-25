// Package httpapi wires the HTTP surface of the service.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Probe reports the reachability of one external dependency.
type Probe interface {
	Name() string
	Required() bool
	Check(ctx context.Context) error
}

// Options configures the router.
type Options struct {
	Probes       []Probe
	ProbeTimeout time.Duration
	Logger       *slog.Logger
}

const defaultProbeTimeout = 2 * time.Second

// New builds the HTTP handler for the service.
func New(opts Options) http.Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	timeout := opts.ProbeTimeout
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}

	h := &health{probes: opts.Probes, timeout: timeout, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", h.live)
	mux.HandleFunc("GET /health/ready", h.ready)
	mux.HandleFunc("/", notFound)

	return mux
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
