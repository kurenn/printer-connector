package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
	"printer-connector/internal/driver"
)

// capDriver advertises a fixed capability set and records which actions were
// actually invoked, so a test can prove an unsupported action never reached the
// printer.
type capDriver struct {
	caps []string

	mu             sync.Mutex
	historyCalled  bool
	pauseCalled    bool
	lastHistoryLim int
}

func (c *capDriver) Capabilities() []string { return c.caps }
func (c *capDriver) Telemetry(_ context.Context) (driver.Telemetry, error) {
	return driver.Telemetry{}, nil
}
func (c *capDriver) QueryObjects(_ context.Context) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}
func (c *capDriver) NormalizeRaw(_ map[string]any) driver.Telemetry { return driver.Telemetry{} }
func (c *capDriver) Pause(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pauseCalled = true
	return nil
}
func (c *capDriver) Resume(_ context.Context) error                         { return nil }
func (c *capDriver) Cancel(_ context.Context) error                         { return nil }
func (c *capDriver) Home(_ context.Context, _ ...string) error              { return nil }
func (c *capDriver) StartPrint(_ context.Context, _ string) error           { return nil }
func (c *capDriver) UploadFile(_ context.Context, _ string, _ []byte) error { return nil }
func (c *capDriver) DeleteFile(_ context.Context, _ string) error           { return nil }
func (c *capDriver) ListFiles(_ context.Context) ([]map[string]any, error)  { return nil, nil }
func (c *capDriver) GetWebcamSnapshot(_ context.Context) ([]byte, string, error) {
	return nil, "", nil
}
func (c *capDriver) GetHistory(_ context.Context, limit int) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.historyCalled = true
	c.lastHistoryLim = limit
	return map[string]any{"result": map[string]any{"jobs": []any{}}}, nil
}

var _ driver.Driver = (*capDriver)(nil)

// commandHarness serves exactly one command and captures its completion.
func commandHarness(t *testing.T, action string, d driver.Driver) (*cloud.CommandCompleteRequest, func()) {
	t.Helper()

	var (
		mu        sync.Mutex
		served    bool
		completed cloud.CommandCompleteRequest
	)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)

	mux.HandleFunc("/api/v1/connectors/1/commands", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if served {
			_, _ = io.WriteString(w, `[]`)
			return
		}
		served = true
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": "4242", "printer_id": 1, "action": action, "params": map[string]any{},
		}})
	})
	mux.HandleFunc("/api/v1/commands/4242/complete", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&completed)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	})
	mux.HandleFunc("/api/v1/snapshots/batch", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"inserted":1}`)
	})

	a := &Agent{
		log:     discardLogger(),
		cfg:     &config.Config{ConnectorID: "1", Printers: []config.Printer{{PrinterID: 1}}},
		drivers: map[int]driver.Driver{1: d},
		cloud:   cloud.New(cloud.Options{BaseURL: srv.URL, ConnectorID: "1", Logger: discardLogger()}),
	}
	if err := a.pollAndExecuteCommands(context.Background()); err != nil {
		srv.Close()
		t.Fatalf("pollAndExecuteCommands: %v", err)
	}

	mu.Lock()
	got := completed
	mu.Unlock()
	return &got, srv.Close
}

// A Bambu-shaped driver (no import_history) must have the command refused
// before it reaches the driver, rather than attempted and failed with a
// protocol-specific error.
func TestUnsupportedActionIsRejectedWithoutTouchingDriver(t *testing.T) {
	d := &capDriver{caps: []string{"pause", "resume", "cancel", "start_print"}}
	completed, closeSrv := commandHarness(t, "import_history", d)
	defer closeSrv()

	d.mu.Lock()
	called := d.historyCalled
	d.mu.Unlock()
	if called {
		t.Error("driver.GetHistory was invoked for an unsupported action")
	}
	if completed.Status != "failed" {
		t.Errorf("status = %q, want failed", completed.Status)
	}
	if completed.ErrorMessage != `printer does not support "import_history"` {
		t.Errorf("error = %q, want the capability-rejection message", completed.ErrorMessage)
	}
	// The reported capabilities make "why was this refused?" answerable.
	if _, ok := completed.Result["supported"]; !ok {
		t.Error("completion result should report the printer's supported actions")
	}
}

// Gating must not block actions the driver does advertise.
func TestSupportedActionStillExecutes(t *testing.T) {
	d := &capDriver{caps: []string{"pause", "resume", "cancel"}}
	completed, closeSrv := commandHarness(t, "pause", d)
	defer closeSrv()

	d.mu.Lock()
	called := d.pauseCalled
	d.mu.Unlock()
	if !called {
		t.Error("driver.Pause was not invoked for a supported action")
	}
	if completed.Status != "succeeded" {
		t.Errorf("status = %q, want succeeded", completed.Status)
	}
}

// A driver that advertises the action gets it dispatched normally — proving the
// gate keys off Capabilities() and not off the driver's concrete type.
func TestGatingKeysOffCapabilitiesNotType(t *testing.T) {
	d := &capDriver{caps: []string{"import_history"}}
	completed, closeSrv := commandHarness(t, "import_history", d)
	defer closeSrv()

	d.mu.Lock()
	called := d.historyCalled
	d.mu.Unlock()
	if !called {
		t.Error("driver.GetHistory should run when the driver advertises import_history")
	}
	if completed.Status != "succeeded" {
		t.Errorf("status = %q, want succeeded", completed.Status)
	}
}

// fetch_gcode is performed by the agent, not the driver, so it must not be
// gated on capabilities (no driver advertises it).
func TestAgentLevelActionIsNotCapabilityGated(t *testing.T) {
	if capabilityGated["fetch_gcode"] {
		t.Error("fetch_gcode must not be capability-gated — no driver advertises it")
	}
}

// MARK: webcam gating
//
// The webcam SNAPSHOT path called the driver without consulting Capabilities(),
// so a Bambu — whose camera is a model-specific stream rather than an HTTP
// snapshot — failed on every request and logged an error each time the cloud
// re-issued it.

// webcamDriver records whether the snapshot was attempted.
type webcamDriver struct {
	capDriver
	mu        sync.Mutex
	attempted bool
}

func (w *webcamDriver) GetWebcamSnapshot(_ context.Context) ([]byte, string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.attempted = true
	return []byte("jpg"), "image/jpeg", nil
}

func TestWebcamRequestIsRefusedWhenUnsupported(t *testing.T) {
	var (
		mu         sync.Mutex
		markedFail bool
		uploaded   bool
	)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/api/v1/webcam_requests/77/fail", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		markedFail = true
		mu.Unlock()
		_, _ = io.WriteString(w, `{}`)
	})
	mux.HandleFunc("/api/v1/webcam_requests/77/upload", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		uploaded = true
		mu.Unlock()
		_, _ = io.WriteString(w, `{}`)
	})

	d := &webcamDriver{capDriver: capDriver{caps: []string{"pause", "resume"}}} // no webcam
	a := &Agent{
		log:     discardLogger(),
		cfg:     &config.Config{ConnectorID: "1", Printers: []config.Printer{{PrinterID: 1}}},
		drivers: map[int]driver.Driver{1: d},
		cloud:   cloud.New(cloud.Options{BaseURL: srv.URL, ConnectorID: "1", Logger: discardLogger()}),
	}

	err := a.handleWebcamRequest(context.Background(),
		cloud.WebcamRequest{ID: cloud.StringOrNumber("77"), PrinterID: 1})

	// Handled, not a processing error — the printer just doesn't offer webcam.
	if err != nil {
		t.Errorf("expected the request to be handled, got error: %v", err)
	}
	d.mu.Lock()
	attempted := d.attempted
	d.mu.Unlock()
	if attempted {
		t.Error("the driver's webcam snapshot must not be attempted when unsupported")
	}
	mu.Lock()
	defer mu.Unlock()
	if !markedFail {
		t.Error("the request must be marked failed so the cloud stops re-queueing it")
	}
	if uploaded {
		t.Error("nothing should be uploaded for an unsupported webcam request")
	}
}

// A printer that does advertise webcam must still be served.
func TestWebcamRequestProceedsWhenSupported(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/api/v1/webcam_requests/78/upload", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	})

	d := &webcamDriver{capDriver: capDriver{caps: []string{"webcam"}}}
	a := &Agent{
		log:     discardLogger(),
		cfg:     &config.Config{ConnectorID: "1", Printers: []config.Printer{{PrinterID: 1}}},
		drivers: map[int]driver.Driver{1: d},
		cloud:   cloud.New(cloud.Options{BaseURL: srv.URL, ConnectorID: "1", Logger: discardLogger()}),
	}
	if err := a.handleWebcamRequest(context.Background(),
		cloud.WebcamRequest{ID: cloud.StringOrNumber("78"), PrinterID: 1}); err != nil {
		t.Fatalf("handleWebcamRequest: %v", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.attempted {
		t.Error("a webcam-capable printer should have its snapshot fetched")
	}
}

// MARK: driver warm-up
//
// Lazily-connecting drivers (Bambu dials MQTT, then waits for the printer's
// first pushed report) couldn't answer the first snapshot cycle, so the batch
// went out without them and the cloud read the printer as offline for a cycle —
// indefinitely if the agent kept restarting.

// countingDriver records how many times Telemetry was called.
type countingDriver struct {
	capDriver
	mu    sync.Mutex
	calls int
}

func (c *countingDriver) Telemetry(_ context.Context) (driver.Telemetry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return driver.Telemetry{}, nil
}

func TestWarmUpOpensEveryDriverSession(t *testing.T) {
	d1 := &countingDriver{}
	d2 := &countingDriver{}
	a := &Agent{
		log:     discardLogger(),
		cfg:     &config.Config{ConnectorID: "1"},
		drivers: map[int]driver.Driver{1: d1, 2: d2},
	}

	a.warmUpDrivers(context.Background())

	for i, d := range []*countingDriver{d1, d2} {
		d.mu.Lock()
		calls := d.calls
		d.mu.Unlock()
		if calls != 1 {
			t.Errorf("driver %d: Telemetry called %d times, want exactly 1", i+1, calls)
		}
	}
}

// Warm-up must not hang or panic when a printer is simply unreachable — an
// offline printer at startup shouldn't hold up the loops.
func TestWarmUpToleratesFailingDrivers(t *testing.T) {
	a := &Agent{
		log:     discardLogger(),
		cfg:     &config.Config{ConnectorID: "1"},
		drivers: map[int]driver.Driver{1: &failingDriver{err: errors.New("unreachable")}},
	}
	done := make(chan struct{})
	go func() { a.warmUpDrivers(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("warm-up hung on an unreachable printer")
	}
}
