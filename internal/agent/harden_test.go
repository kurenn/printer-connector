package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
	"printer-connector/internal/driver"
	"printer-connector/internal/moonraker"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSnapshotStalled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		lastPush  time.Time
		threshold time.Duration
		want      bool
	}{
		{"fresh push", now.Add(-10 * time.Second), 90 * time.Second, false},
		{"exactly at threshold", now.Add(-90 * time.Second), 90 * time.Second, false},
		{"just over threshold", now.Add(-91 * time.Second), 90 * time.Second, true},
		{"long stall", now.Add(-48 * time.Minute), 90 * time.Second, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotStalled(tc.lastPush, now, tc.threshold); got != tc.want {
				t.Fatalf("snapshotStalled = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunGuardedRecoversPanic(t *testing.T) {
	t.Parallel()

	a := &Agent{log: discardLogger()}
	err := a.runGuarded("snapshots", func(context.Context) error {
		panic("boom")
	}, context.Background())

	if err == nil {
		t.Fatal("expected an error from a panicking loop body, got nil")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("error should name the panic, got: %v", err)
	}
}

func TestRunGuardedPassesThroughNormalReturn(t *testing.T) {
	t.Parallel()

	a := &Agent{log: discardLogger()}
	sentinel := errors.New("transient")
	if err := a.runGuarded("x", func(context.Context) error { return sentinel }, context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error to pass through, got %v", err)
	}
}

func TestSuperviseLoopStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	a := &Agent{log: discardLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- a.superviseLoop(ctx, "test", func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
	}()

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled after cancel, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("superviseLoop did not return after context cancel")
	}
}

// A loop body that faults (panics or returns an error) must be restarted, not
// abandoned — otherwise the loop dies silently. restartDelay is left at its
// zero value so restarts are immediate.
func TestSuperviseLoopRestartsAfterFault(t *testing.T) {
	t.Parallel()

	a := &Agent{log: discardLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		_ = a.superviseLoop(ctx, "test", func(ctx context.Context) error {
			if calls.Add(1) >= 3 {
				cancel() // stop after we've proven it restarts
				<-ctx.Done()
				return ctx.Err()
			}
			panic("transient fault") // first invocations panic; supervisor must recover + restart
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("superviseLoop did not stop after cancel")
	}
	if got := calls.Load(); got < 3 {
		t.Fatalf("expected the loop body to be restarted (>=3 invocations), got %d", got)
	}
}

// When every configured printer fails to answer, the cycle must surface an
// error (not silently succeed) and must not advance the stall clock — that is
// what keeps the watchdog able to detect the stall.
func TestCollectAndPushSnapshotsErrorsWhenAllPrintersFail(t *testing.T) {
	t.Parallel()

	moon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer moon.Close()

	a := &Agent{
		log:     discardLogger(),
		cfg:     &config.Config{Printers: []config.Printer{{PrinterID: 1, BaseURL: moon.URL}}},
		drivers: map[int]driver.Driver{1: moonraker.New(moon.URL, 80)},
		cloud:   cloud.New(cloud.Options{BaseURL: "http://127.0.0.1:1"}), // never reached
	}

	err := a.collectAndPushSnapshots(context.Background())
	if err == nil {
		t.Fatal("expected an error when all printers fail to answer, got nil")
	}
	if a.lastSnapshotPushUnix.Load() != 0 {
		t.Fatalf("stall clock must not advance on a failed cycle, got %d", a.lastSnapshotPushUnix.Load())
	}
}

func TestCollectAndPushSnapshotsRecordsSuccessfulPush(t *testing.T) {
	t.Parallel()

	moon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":{"status":{}}}`)
	}))
	defer moon.Close()

	var pushed bool
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/snapshots/batch" {
			pushed = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"inserted":1}`)
	}))
	defer cloudSrv.Close()

	a := &Agent{
		log:     discardLogger(),
		cfg:     &config.Config{Printers: []config.Printer{{PrinterID: 1, BaseURL: moon.URL}}},
		drivers: map[int]driver.Driver{1: moonraker.New(moon.URL, 80)},
		cloud:   cloud.New(cloud.Options{BaseURL: cloudSrv.URL}),
	}

	before := time.Now().Add(-time.Second).UnixNano()
	if err := a.collectAndPushSnapshots(context.Background()); err != nil {
		t.Fatalf("expected a successful push, got %v", err)
	}
	if !pushed {
		t.Fatal("expected a snapshot batch to be pushed to the cloud")
	}
	if a.lastSnapshotPushUnix.Load() < before {
		t.Fatalf("stall clock should advance on a successful push, got %d", a.lastSnapshotPushUnix.Load())
	}
}
