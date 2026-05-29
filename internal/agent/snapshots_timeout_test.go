package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
	"printer-connector/internal/driver"
	"printer-connector/internal/moonraker"
)

// slowDriver is a driver.Driver fake whose QueryObjects blocks until either
// `delay` elapses or the context is cancelled. Every other method is a no-op
// so the call sites in agent code can still treat it like a real driver.
type slowDriver struct {
	delay   time.Duration
	calls   atomic.Int32
	context atomic.Int32 // counts ctx-cancelled returns
}

func (s *slowDriver) Capabilities() []string { return nil }

func (s *slowDriver) Telemetry(_ context.Context) (driver.Telemetry, error) {
	return driver.Telemetry{}, nil
}

func (s *slowDriver) QueryObjects(ctx context.Context) (map[string]any, error) {
	s.calls.Add(1)
	select {
	case <-time.After(s.delay):
		return map[string]any{}, nil
	case <-ctx.Done():
		s.context.Add(1)
		return nil, ctx.Err()
	}
}

func (s *slowDriver) NormalizeRaw(_ map[string]any) driver.Telemetry              { return driver.Telemetry{} }
func (s *slowDriver) Pause(_ context.Context) error                               { return nil }
func (s *slowDriver) Resume(_ context.Context) error                              { return nil }
func (s *slowDriver) Cancel(_ context.Context) error                              { return nil }
func (s *slowDriver) Home(_ context.Context, _ ...string) error                   { return nil }
func (s *slowDriver) StartPrint(_ context.Context, _ string) error                { return nil }
func (s *slowDriver) UploadFile(_ context.Context, _ string, _ []byte) error      { return nil }
func (s *slowDriver) DeleteFile(_ context.Context, _ string) error                { return nil }
func (s *slowDriver) ListFiles(_ context.Context) ([]map[string]any, error)       { return nil, nil }
func (s *slowDriver) GetHistory(_ context.Context, _ int) (map[string]any, error) { return nil, nil }
func (s *slowDriver) GetWebcamSnapshot(_ context.Context) ([]byte, string, error) {
	return nil, "", nil
}

var _ driver.Driver = (*slowDriver)(nil)

// One unresponsive printer must NOT stall the rest of the snapshot cycle: the
// loop wraps each QueryObjects in a per-call context.WithTimeout, so a wedged
// printer is recorded as unreachable within `pollTimeout` and the responsive
// printers go on to produce telemetry as usual.
func TestCollectAndPushSnapshotsTimesOutWedgedPrinter(t *testing.T) {
	t.Parallel()

	// Healthy Moonraker — answers instantly.
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

	// Slow driver simulates a wedged printer: would block for 10 s, but the
	// per-call timeout below should fire after ~50 ms.
	slow := &slowDriver{delay: 10 * time.Second}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "connector.json")
	cfg := &config.Config{
		SiteName: "test",
		Printers: []config.Printer{
			{PrinterID: 1, Type: config.TypeMoonraker, BaseURL: moon.URL},
			{PrinterID: 2, Type: config.TypeMoonraker, BaseURL: "stub-for-slow"},
		},
	}

	a := &Agent{
		log:         discardLogger(),
		cfg:         cfg,
		cfgPath:     cfgPath,
		drivers:     map[int]driver.Driver{1: moonraker.New(moon.URL, 80), 2: slow},
		cloud:       cloud.New(cloud.Options{BaseURL: cloudSrv.URL}),
		pollTimeout: 50 * time.Millisecond, // short, for the test
	}

	start := time.Now()
	if err := a.collectAndPushSnapshots(context.Background()); err != nil {
		t.Fatalf("expected the cycle to succeed (healthy printer pushed), got %v", err)
	}
	elapsed := time.Since(start)

	// Must complete in roughly pollTimeout, NOT slow.delay.
	if elapsed > 2*time.Second {
		t.Fatalf("loop took %v — the slow printer stalled the cycle; per-call timeout did not fire", elapsed)
	}
	if !pushed {
		t.Fatal("expected the healthy printer's snapshot to still be pushed to the cloud")
	}
	if slow.calls.Load() != 1 {
		t.Fatalf("slow driver should be queried exactly once, got %d", slow.calls.Load())
	}
	if slow.context.Load() != 1 {
		t.Fatalf("slow driver should have observed a ctx-cancelled return, got %d", slow.context.Load())
	}

	// status.json: the slow printer is reachable=false; the healthy one is reachable=true.
	sf := readStatusFile(t, dir)
	if len(sf.Printers) != 2 {
		t.Fatalf("status.json should list both printers, got %d", len(sf.Printers))
	}
	byID := map[int]printerStatus{}
	for _, p := range sf.Printers {
		byID[p.PrinterID] = p
	}
	if !byID[1].Reachable {
		t.Errorf("healthy printer 1 should be reachable=true")
	}
	if byID[2].Reachable {
		t.Errorf("wedged printer 2 should be reachable=false, got reachable=true")
	}
	if byID[2].State != driver.StateOffline {
		t.Errorf("wedged printer 2 state should be %q, got %q", driver.StateOffline, byID[2].State)
	}
}

// Sanity check: without the override the default poll timeout is the package
// constant — guards against an accidental rename or zero-default regression.
func TestDefaultPollTimeoutIsTenSeconds(t *testing.T) {
	t.Parallel()
	if defaultPollTimeout != 10*time.Second {
		t.Fatalf("defaultPollTimeout = %v, want 10s", defaultPollTimeout)
	}
}
