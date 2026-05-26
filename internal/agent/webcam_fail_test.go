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

	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
	"printer-connector/internal/driver"
)

// failingDriver always returns an error from GetWebcamSnapshot so we can test
// the failure path without a real Moonraker instance. All other methods are
// no-ops that satisfy the driver.Driver interface.
type failingDriver struct{ err error }

func (f *failingDriver) Capabilities() []string                                      { return nil }
func (f *failingDriver) Telemetry(_ context.Context) (driver.Telemetry, error)       { return driver.Telemetry{}, nil }
func (f *failingDriver) QueryObjects(_ context.Context) (map[string]any, error)      { return nil, nil }
func (f *failingDriver) NormalizeRaw(_ map[string]any) driver.Telemetry              { return driver.Telemetry{} }
func (f *failingDriver) Pause(_ context.Context) error                               { return nil }
func (f *failingDriver) Resume(_ context.Context) error                              { return nil }
func (f *failingDriver) Cancel(_ context.Context) error                              { return nil }
func (f *failingDriver) Home(_ context.Context, _ ...string) error                   { return nil }
func (f *failingDriver) StartPrint(_ context.Context, _ string) error                { return nil }
func (f *failingDriver) UploadFile(_ context.Context, _ string, _ []byte) error      { return nil }
func (f *failingDriver) DeleteFile(_ context.Context, _ string) error                { return nil }
func (f *failingDriver) ListFiles(_ context.Context) ([]map[string]any, error)       { return nil, nil }
func (f *failingDriver) GetHistory(_ context.Context, _ int) (map[string]any, error) { return nil, nil }
func (f *failingDriver) GetWebcamSnapshot(_ context.Context) ([]byte, string, error) {
	return nil, "", f.err
}

var _ driver.Driver = (*failingDriver)(nil)

// TestHandleWebcamRequest_MarkFailedOnSnapshotError verifies that when
// GetWebcamSnapshot returns an error, handleWebcamRequest calls the cloud
// /fail endpoint with the request ID and the error message.
func TestHandleWebcamRequest_MarkFailedOnSnapshotError(t *testing.T) {
	t.Parallel()

	const printerID = 42
	const requestID = "99"
	snapshotErr := errors.New("GET /webcam: 404 not found")

	var (
		mu            sync.Mutex
		failCalled    bool
		failRequestID string
		failBody      map[string]string
	)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Cloud endpoint: record /fail calls.
	mux.HandleFunc("/api/v1/webcam_requests/"+requestID+"/fail", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		failCalled = true
		failRequestID = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &failBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"failed"}`)
	})

	a := &Agent{
		log:     discardLogger(),
		cfg:     &config.Config{Printers: []config.Printer{{PrinterID: printerID}}},
		drivers: map[int]driver.Driver{printerID: &failingDriver{err: snapshotErr}},
		cloud:   cloud.New(cloud.Options{BaseURL: srv.URL, ConnectorID: "1", Logger: discardLogger()}),
	}

	req := cloud.WebcamRequest{
		ID:        cloud.StringOrNumber(requestID),
		PrinterID: printerID,
	}

	err := a.handleWebcamRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected handleWebcamRequest to return an error on snapshot failure, got nil")
	}
	if !errors.Is(err, snapshotErr) {
		t.Fatalf("expected snapshot error to propagate, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if !failCalled {
		t.Fatal("expected MarkWebcamRequestFailed to be called, but the /fail endpoint was never hit")
	}
	if failRequestID != "/api/v1/webcam_requests/"+requestID+"/fail" {
		t.Errorf("fail endpoint called with wrong path: %q", failRequestID)
	}
	if failBody["error_message"] != snapshotErr.Error() {
		t.Errorf("error_message = %q, want %q", failBody["error_message"], snapshotErr.Error())
	}
}
