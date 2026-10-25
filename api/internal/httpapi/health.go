package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Readiness statuses reported in the body.
const (
	statusOK          = "ok"
	statusDegraded    = "degraded"
	statusUnavailable = "unavailable"

	checkOK    = "ok"
	checkError = "error"
)

type checkResult struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
}

type readyResponse struct {
	Status string        `json:"status"`
	Checks []checkResult `json:"checks"`
}

type health struct {
	probes  []Probe
	timeout time.Duration
	logger  *slog.Logger
}

// live answers without touching any dependency: a liveness failure must mean the
// process is broken, not that a dependency is slow.
func (h *health) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": statusOK})
}

func (h *health) ready(w http.ResponseWriter, r *http.Request) {
	checks := make([]checkResult, 0, len(h.probes))
	requiredFailed := false
	optionalFailed := false

	for _, probe := range h.probes {
		result := checkResult{Name: probe.Name(), Status: checkOK, Required: probe.Required()}
		if err := h.check(r.Context(), probe); err != nil {
			result.Status = checkError
			if probe.Required() {
				requiredFailed = true
			} else {
				optionalFailed = true
			}
			h.logger.Warn("readiness probe failed",
				slog.String("dependency", probe.Name()),
				slog.Bool("required", probe.Required()),
				slog.String("error", err.Error()),
			)
		}
		checks = append(checks, result)
	}

	status := statusOK
	if optionalFailed {
		status = statusDegraded
	}
	if requiredFailed {
		status = statusUnavailable
	}

	code := http.StatusOK
	if status != statusOK {
		code = http.StatusServiceUnavailable
	}

	writeJSON(w, code, readyResponse{Status: status, Checks: checks})
}

func (h *health) check(ctx context.Context, probe Probe) error {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	return probe.Check(ctx)
}
