package cloud

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestClient builds a Client wired to the given test server.
func newTestClient(baseURL string) *Client {
	return New(Options{
		BaseURL:         baseURL,
		ConnectorID:     "conn-42",
		ConnectorSecret: "secret-abc",
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		UserAgent:       "test/1.0",
	})
}

// decodeBody reads and JSON-decodes the request body into v.
func decodeBody(t *testing.T, r *http.Request, v any) {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

// ---------- Register ----------

func TestRegister_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/connectors/register" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		var req RegisterRequest
		decodeBody(t, r, &req)
		if req.PairingToken == "" {
			t.Error("pairing_token must not be empty")
		}
		if req.Device.Hostname == "" {
			t.Error("device.hostname must not be empty")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"connector":    {"id": 7},
			"credentials":  {"secret": "sup3r-secret"},
			"printers":     [{"id": 1, "name": "Voron"}],
			"polling":      {"commands_seconds": 5, "snapshots_seconds": 10}
		}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	resp, err := c.Register(context.Background(), RegisterRequest{
		PairingToken: "tok-123",
		Device:       DeviceInfo{Hostname: "raspi"},
		Printers:     []PrinterInfo{{Name: "Voron", Type: "moonraker"}},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.Connector.ID.String() != "7" {
		t.Errorf("connector.id = %q, want \"7\"", resp.Connector.ID.String())
	}
	if resp.Credentials.Secret != "sup3r-secret" {
		t.Errorf("credentials.secret = %q, want sup3r-secret", resp.Credentials.Secret)
	}
	if len(resp.Printers) != 1 || resp.Printers[0].Name != "Voron" {
		t.Errorf("printers = %v, want [{1 Voron}]", resp.Printers)
	}
	if resp.Polling.CommandsSeconds != 5 || resp.Polling.SnapshotsSeconds != 10 {
		t.Errorf("polling = %+v, want {5 10}", resp.Polling)
	}
}

func TestRegister_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Register(context.Background(), RegisterRequest{PairingToken: "bad"})
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q should mention 401", err.Error())
	}
}

// ---------- Heartbeat ----------

func TestHeartbeat_Success(t *testing.T) {
	var capturedPath string
	var capturedAuth string
	var body HeartbeatRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		decodeBody(t, r, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	hb := HeartbeatRequest{}
	hb.Status.UptimeSeconds = 3600
	hb.Status.Version = "1.2.3"
	hb.Printers = []HeartbeatPrinter{{PrinterID: 1, Reachable: true}}

	if err := c.Heartbeat(context.Background(), hb); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if capturedPath != "/api/v1/connectors/conn-42/heartbeat" {
		t.Errorf("path = %q, want /api/v1/connectors/conn-42/heartbeat", capturedPath)
	}
	if capturedAuth != "Bearer secret-abc" {
		t.Errorf("Authorization = %q, want Bearer secret-abc", capturedAuth)
	}
	if body.Status.UptimeSeconds != 3600 {
		t.Errorf("uptime_seconds = %d, want 3600", body.Status.UptimeSeconds)
	}
	if len(body.Printers) != 1 || !body.Printers[0].Reachable {
		t.Errorf("printers = %v, want [{1 true}]", body.Printers)
	}
}

func TestHeartbeat_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	err := c.Heartbeat(context.Background(), HeartbeatRequest{})
	if err == nil {
		t.Fatal("expected error for 503 response")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error %q should mention 503", err.Error())
	}
}

// ---------- PushSnapshots ----------

func TestPushSnapshots_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/snapshots/batch" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret-abc" {
			t.Errorf("missing auth header, got %q", r.Header.Get("Authorization"))
		}

		var req SnapshotsBatchRequest
		decodeBody(t, r, &req)
		if len(req.Snapshots) != 2 {
			t.Errorf("got %d snapshots, want 2", len(req.Snapshots))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"inserted": 2}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	resp, err := c.PushSnapshots(context.Background(), SnapshotsBatchRequest{
		Snapshots: []Snapshot{
			{PrinterID: 1, CapturedAt: "2024-01-01T00:00:00Z", Payload: map[string]any{"state": "idle"}},
			{PrinterID: 2, CapturedAt: "2024-01-01T00:00:01Z", Payload: map[string]any{"state": "printing"}},
		},
	})
	if err != nil {
		t.Fatalf("PushSnapshots: %v", err)
	}
	if resp.Inserted != 2 {
		t.Errorf("inserted = %d, want 2", resp.Inserted)
	}
}

func TestPushSnapshots_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.PushSnapshots(context.Background(), SnapshotsBatchRequest{})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------- GetCommands ----------

func TestGetCommands_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v1/connectors/conn-42/commands") {
			t.Errorf("path = %q, unexpected", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "5" {
			t.Errorf("limit query = %q, want 5", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id": 1, "printer_id": 10, "action": "pause"}, {"id": "2", "printer_id": 11, "action": "resume"}]`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	cmds, err := c.GetCommands(context.Background(), "conn-42", 5)
	if err != nil {
		t.Fatalf("GetCommands: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want 2", len(cmds))
	}
	if cmds[0].ID.String() != "1" || cmds[0].Action != "pause" {
		t.Errorf("cmds[0] = %+v, want {id:1 action:pause}", cmds[0])
	}
	if cmds[1].ID.String() != "2" || cmds[1].Action != "resume" {
		t.Errorf("cmds[1] = %+v, want {id:2 action:resume}", cmds[1])
	}
}

func TestGetCommands_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.GetCommands(context.Background(), "conn-42", 10)
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

// ---------- CompleteCommand ----------

func TestCompleteCommand_Success(t *testing.T) {
	var capturedPath string
	var body CommandCompleteRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		decodeBody(t, r, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	id := StringOrNumber("99")
	err := c.CompleteCommand(context.Background(), id, CommandCompleteRequest{
		Status: "completed",
		Result: map[string]any{"ok": true},
	})
	if err != nil {
		t.Fatalf("CompleteCommand: %v", err)
	}
	if capturedPath != "/api/v1/commands/99/complete" {
		t.Errorf("path = %q, want /api/v1/commands/99/complete", capturedPath)
	}
	if body.Status != "completed" {
		t.Errorf("status = %q, want completed", body.Status)
	}
}

// ---------- GetWebcamRequests ----------

func TestGetWebcamRequests_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Query().Get("limit") != "3" {
			t.Errorf("limit = %q, want 3", r.URL.Query().Get("limit"))
		}
		if r.Header.Get("X-Connector-Id") != "conn-42" {
			t.Errorf("X-Connector-Id = %q, want conn-42", r.Header.Get("X-Connector-Id"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id": 5, "printer_id": 1}, {"id": "6", "printer_id": 2}]`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	reqs, err := c.GetWebcamRequests(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetWebcamRequests: %v", err)
	}
	if len(reqs) != 2 {
		t.Fatalf("got %d requests, want 2", len(reqs))
	}
	if reqs[0].ID.String() != "5" || reqs[0].PrinterID != 1 {
		t.Errorf("reqs[0] = %+v, want {5 1}", reqs[0])
	}
	if reqs[1].ID.String() != "6" || reqs[1].PrinterID != 2 {
		t.Errorf("reqs[1] = %+v, want {6 2}", reqs[1])
	}
}

func TestGetWebcamRequests_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	reqs, err := c.GetWebcamRequests(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetWebcamRequests empty: %v", err)
	}
	if len(reqs) != 0 {
		t.Errorf("got %d requests, want 0", len(reqs))
	}
}

// ---------- GetWebcamStreamRequests ----------

func TestGetWebcamStreamRequests_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1/connectors/conn-42/webcam_stream"
		if r.URL.Path != wantPath {
			t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"printer_id": 3, "expires_in_ms": 5000}]`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	reqs, err := c.GetWebcamStreamRequests(context.Background())
	if err != nil {
		t.Fatalf("GetWebcamStreamRequests: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	if reqs[0].PrinterID != 3 || reqs[0].ExpiresInMs != 5000 {
		t.Errorf("reqs[0] = %+v, want {3 5000}", reqs[0])
	}
}

func TestGetWebcamStreamRequests_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	reqs, err := c.GetWebcamStreamRequests(context.Background())
	if err != nil {
		t.Fatalf("GetWebcamStreamRequests empty: %v", err)
	}
	if len(reqs) != 0 {
		t.Errorf("got %d requests, want 0", len(reqs))
	}
}

// ---------- MarkWebcamRequestFailed ----------

func TestMarkWebcamRequestFailed_Success(t *testing.T) {
	var capturedPath string
	var body map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		decodeBody(t, r, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	id := StringOrNumber("77")
	err := c.MarkWebcamRequestFailed(context.Background(), id, "camera timeout")
	if err != nil {
		t.Fatalf("MarkWebcamRequestFailed: %v", err)
	}
	if capturedPath != "/api/v1/webcam_requests/77/fail" {
		t.Errorf("path = %q, want /api/v1/webcam_requests/77/fail", capturedPath)
	}
	if body["error_message"] != "camera timeout" {
		t.Errorf("error_message = %q, want camera timeout", body["error_message"])
	}
}

// ---------- UploadWebcamSnapshot ----------

func TestUploadWebcamSnapshot_SendsImageWithHeaders(t *testing.T) {
	var capturedPath, capturedCT, capturedPrinterID string
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedCT = r.Header.Get("Content-Type")
		capturedPrinterID = r.Header.Get("X-Printer-Id")
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	id := StringOrNumber("12")
	imageData := []byte{0xFF, 0xD8, 0xFF} // JPEG magic bytes

	err := c.UploadWebcamSnapshot(context.Background(), id, 5, imageData, "image/jpeg")
	if err != nil {
		t.Fatalf("UploadWebcamSnapshot: %v", err)
	}
	if capturedPath != "/api/v1/webcam_requests/12/upload" {
		t.Errorf("path = %q, want /api/v1/webcam_requests/12/upload", capturedPath)
	}
	if capturedCT != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", capturedCT)
	}
	if capturedPrinterID != "5" {
		t.Errorf("X-Printer-Id = %q, want 5", capturedPrinterID)
	}
	if string(capturedBody) != string(imageData) {
		t.Errorf("body mismatch: got %v, want %v", capturedBody, imageData)
	}
}

func TestUploadWebcamSnapshot_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unprocessable entity", http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	id := StringOrNumber("1")
	err := c.UploadWebcamSnapshot(context.Background(), id, 1, []byte("img"), "image/jpeg")
	if err == nil {
		t.Fatal("expected error for 422 response")
	}
	if !strings.Contains(err.Error(), "422") {
		t.Errorf("error %q should mention 422", err.Error())
	}
}

// ---------- UploadWebcamStreamFrame ----------

func TestUploadWebcamStreamFrame_Success(t *testing.T) {
	var capturedMethod, capturedPath, capturedCT string
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedCT = r.Header.Get("Content-Type")
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	frame := []byte("mjpeg-frame-data")
	err := c.UploadWebcamStreamFrame(context.Background(), 9, frame, "image/jpeg")
	if err != nil {
		t.Fatalf("UploadWebcamStreamFrame: %v", err)
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", capturedMethod)
	}
	if capturedPath != "/api/v1/printers/9/webcam_stream_frame" {
		t.Errorf("path = %q, want /api/v1/printers/9/webcam_stream_frame", capturedPath)
	}
	if capturedCT != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", capturedCT)
	}
	if string(capturedBody) != string(frame) {
		t.Errorf("frame body mismatch")
	}
}

func TestUploadWebcamStreamFrame_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	err := c.UploadWebcamStreamFrame(context.Background(), 1, []byte("frame"), "image/jpeg")
	if err == nil {
		t.Fatal("expected error for 502 response")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error %q should mention 502", err.Error())
	}
}

// ---------- UploadBackup ----------

func TestUploadBackup_Success(t *testing.T) {
	var capturedMethod string
	var capturedCT string
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedCT = r.Header.Get("Content-Type")
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Write a temp file to upload.
	dir := t.TempDir()
	f := filepath.Join(dir, "backup.tar.gz")
	if err := os.WriteFile(f, []byte("fake-gz-content"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := newTestClient(srv.URL)
	if err := c.UploadBackup(context.Background(), srv.URL+"/presigned-upload", f); err != nil {
		t.Fatalf("UploadBackup: %v", err)
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", capturedMethod)
	}
	if capturedCT != "application/gzip" {
		t.Errorf("Content-Type = %q, want application/gzip", capturedCT)
	}
	if string(capturedBody) != "fake-gz-content" {
		t.Errorf("body = %q, want fake-gz-content", capturedBody)
	}
}

func TestUploadBackup_MissingFile(t *testing.T) {
	c := newTestClient("http://127.0.0.1:9") // won't be reached
	err := c.UploadBackup(context.Background(), "http://127.0.0.1:9/upload", "/nonexistent/file.tar.gz")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestUploadBackup_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	dir := t.TempDir()
	f := filepath.Join(dir, "b.tar.gz")
	if err := os.WriteFile(f, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := newTestClient(srv.URL)
	err := c.UploadBackup(context.Background(), srv.URL+"/upload", f)
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error %q should mention 403", err.Error())
	}
}

// ---------- UploadGcode ----------

func TestUploadGcode_Success(t *testing.T) {
	var capturedCT string
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCT = r.Header.Get("Content-Type")
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	f := filepath.Join(dir, "model.gcode")
	gcodeContent := []byte("G28\nG1 X100 Y100 Z10\n")
	if err := os.WriteFile(f, gcodeContent, 0o600); err != nil {
		t.Fatal(err)
	}

	c := newTestClient(srv.URL)
	if err := c.UploadGcode(context.Background(), srv.URL+"/presigned-gcode", f); err != nil {
		t.Fatalf("UploadGcode: %v", err)
	}
	if capturedCT != "text/x-gcode" {
		t.Errorf("Content-Type = %q, want text/x-gcode", capturedCT)
	}
	if string(capturedBody) != string(gcodeContent) {
		t.Errorf("body mismatch")
	}
}

func TestUploadGcode_MissingFile(t *testing.T) {
	c := newTestClient("http://127.0.0.1:9")
	err := c.UploadGcode(context.Background(), "http://127.0.0.1:9/upload", "/nonexistent/model.gcode")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestUploadGcode_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	f := filepath.Join(dir, "m.gcode")
	if err := os.WriteFile(f, []byte("G28"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := newTestClient(srv.URL)
	err := c.UploadGcode(context.Background(), srv.URL+"/upload", f)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error %q should mention 404", err.Error())
	}
}

// ---------- SetCredentials ----------

func TestSetCredentials_UpdatesAuthHeaders(t *testing.T) {
	var capturedAuth, capturedConnID string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedConnID = r.Header.Get("X-Connector-Id")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetCredentials("new-id", "new-secret")

	// Trigger any authenticated call — Heartbeat uses authHeaders.
	_ = c.Heartbeat(context.Background(), HeartbeatRequest{})

	if capturedAuth != "Bearer new-secret" {
		t.Errorf("Authorization = %q, want Bearer new-secret", capturedAuth)
	}
	if capturedConnID != "new-id" {
		t.Errorf("X-Connector-Id = %q, want new-id", capturedConnID)
	}
}
