// Package worker provides the interval scheduler that runs the worker
// process's periodic jobs.
//
// The worker's first duty is mail delivery; the scheduler is additive: three
// later features (trending producer, invitation sweeper, taste profile) need
// periodic jobs, and this is the mechanism they register with. It is a ticker
// with a registry, not a cron: jobs run every interval, sequentially, in
// registration order.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type job struct {
	name string
	run  func(ctx context.Context) error
}

// Scheduler runs every registered job on each tick of a configured interval.
// Register is startup wiring: it is not safe to call concurrently with Run.
type Scheduler struct {
	interval time.Duration
	logger   *slog.Logger
	jobs     []job
}

// NewScheduler builds a scheduler that ticks every interval. The interval
// must be positive: a zero or negative one would panic time.NewTicker, and a
// scheduler that cannot tick is a configuration error, not a scheduler.
func NewScheduler(interval time.Duration, logger *slog.Logger) (*Scheduler, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("worker scheduler: interval must be positive, got %s", interval)
	}
	if logger == nil {
		return nil, fmt.Errorf("worker scheduler: logger is required")
	}
	return &Scheduler{interval: interval, logger: logger}, nil
}

// Register appends a job to the registry. A failing job never stops the
// scheduler and never skips the jobs after it; the failure is logged.
func (s *Scheduler) Register(name string, run func(ctx context.Context) error) {
	s.jobs = append(s.jobs, job{name: name, run: run})
}

// Run executes every registered job on each tick until ctx is cancelled.
// Cancellation is the graceful shutdown path: the in-flight job sees the
// cancelled context, the loop returns, and no new tick starts. Jobs that
// ignore the context delay the shutdown; keep them bounded.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.logger.Info("scheduler started",
		slog.String("interval", s.interval.String()),
		slog.Int("jobs", len(s.jobs)),
	)
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler stopped")
			return
		case <-ticker.C:
			s.runTick(ctx)
		}
	}
}

// runTick runs one round of jobs. A cancelled context aborts the round
// between jobs: shutdown must never start new work.
func (s *Scheduler) runTick(ctx context.Context) {
	for _, job := range s.jobs {
		if ctx.Err() != nil {
			return
		}
		if err := job.run(ctx); err != nil {
			s.logger.Error("scheduled job failed",
				slog.String("job", job.name),
				slog.String("error", err.Error()),
			)
		}
	}
}

// Heartbeat is the proof-of-mechanism job: it does no work beyond one debug
// log line, proving the registry, the tick and the shutdown path end to end
// before the first real feature job registers.
func Heartbeat(logger *slog.Logger) (string, func(ctx context.Context) error) {
	return "heartbeat", func(ctx context.Context) error {
		logger.Debug("scheduler heartbeat")
		return nil
	}
}
