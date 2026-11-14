package worker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitFor polls cond until it holds or the deadline passes, so the tests do
// not sleep a fixed, racy amount.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestNewSchedulerRejectsNonPositiveInterval(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		if _, err := NewScheduler(interval, testLogger()); err == nil {
			t.Errorf("NewScheduler(%s) succeeded, want error", interval)
		}
	}
}

func TestSchedulerRunsEveryRegisteredJobEachTickInOrder(t *testing.T) {
	scheduler, err := NewScheduler(5*time.Millisecond, testLogger())
	if err != nil {
		t.Fatalf("NewScheduler returned error: %v", err)
	}

	var mu sync.Mutex
	var calls []string
	record := func(name string) func(context.Context) error {
		return func(context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, name)
			return nil
		}
	}
	scheduler.Register("first", record("first"))
	scheduler.Register("second", record("second"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()

	// Two full ticks is what proves "every job, every tick": one tick could be luck.
	waitFor(t, "two ticks (4 job calls)", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(calls) >= 4
	})
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	for tick := 0; tick < 2; tick++ {
		got := calls[tick*2 : tick*2+2]
		if got[0] != "first" || got[1] != "second" {
			t.Errorf("tick %d ran %v, want [first second] in registration order", tick, got)
		}
	}
}

func TestSchedulerKeepsRunningWhenAJobFails(t *testing.T) {
	scheduler, err := NewScheduler(5*time.Millisecond, testLogger())
	if err != nil {
		t.Fatalf("NewScheduler returned error: %v", err)
	}

	var failed, after int32
	scheduler.Register("failing", func(context.Context) error {
		atomic.AddInt32(&failed, 1)
		return errors.New("boom")
	})
	scheduler.Register("after", func(context.Context) error {
		atomic.AddInt32(&after, 1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()

	// The failing job must not stop the tick, and must not skip the jobs after it.
	waitFor(t, "the job after the failing one to run twice", func() bool {
		return atomic.LoadInt32(&after) >= 2
	})
	cancel()
	<-done

	if atomic.LoadInt32(&failed) < 2 {
		t.Errorf("failing job ran %d times, want at least 2: one failure stopped the scheduler", failed)
	}
}

func TestSchedulerStopsOnContextCancel(t *testing.T) {
	// A long interval: the test never waits for a tick, only for the shutdown.
	scheduler, err := NewScheduler(time.Hour, testLogger())
	if err != nil {
		t.Fatalf("NewScheduler returned error: %v", err)
	}
	scheduler.Register("noop", func(context.Context) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop on context cancel")
	}
}

func TestHeartbeatIsANoopThatLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	name, job := Heartbeat(logger)
	if name == "" {
		t.Error("heartbeat job name is empty")
	}
	if err := job(context.Background()); err != nil {
		t.Errorf("heartbeat job returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "heartbeat") {
		t.Errorf("heartbeat job logged nothing: %q", buf.String())
	}
}
