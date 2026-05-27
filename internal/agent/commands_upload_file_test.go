package agent

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
	"printer-connector/internal/moonraker"
)

// TestExecuteUploadFile_FromURL streams a slicer upload from a signed cloud URL
// into a fake Moonraker, with the print flag set — the connector should download
// the bytes verbatim, POST them to /server/files/upload as multipart, and ask
// Moonraker to start the print (print=true).
func TestExecuteUploadFile_FromURL(t *testing.T) {
	const gcodeBody = "; sliced\nG28\nG1 X10 Y10 E1 F1500\n"

	var (
		mu           sync.Mutex
		gotToken     string
		gotFilename  string
		gotRoot      string
		gotPrint     string
		gotContent   string
		uploadCalled bool
	)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// cloud: signed download URL hands back the parked bytes
	mux.HandleFunc("/api/v1/slicer_uploads/77/download", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotToken = r.URL.Query().Get("token")
		mu.Unlock()
		w.Header().Set("Content-Type", "text/x-gcode")
		_, _ = io.WriteString(w, gcodeBody)
	})

	// moonraker: capture the multipart upload
	mux.HandleFunc("/server/files/upload", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(8 << 20)
		f, hdr, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer f.Close()
		body, _ := io.ReadAll(f)

		mu.Lock()
		uploadCalled = true
		gotFilename = hdr.Filename
		gotRoot = r.FormValue("root")
		gotPrint = r.FormValue("print")
		gotContent = string(body)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":"benchy.gcode"}`)
	})

	a := &Agent{
		log:   discardLogger(),
		cfg:   &config.Config{ConnectorID: "1", CloudURL: srv.URL},
		cloud: cloud.New(cloud.Options{BaseURL: srv.URL, ConnectorID: "1", Logger: discardLogger()}),
	}
	mc := moonraker.New(srv.URL, 80)

	cmd := cloud.Command{
		ID:        "9100",
		PrinterID: 1,
		Action:    "upload_file",
		Params: map[string]any{
			"filename":     "benchy.gcode",
			"download_url": srv.URL + "/api/v1/slicer_uploads/77/download?token=signed-token",
			"print":        true,
		},
	}

	result := map[string]any{}
	if err := a.executeUploadFile(context.Background(), mc, cmd, result); err != nil {
		t.Fatalf("executeUploadFile: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !uploadCalled {
		t.Fatal("moonraker upload was never called")
	}
	if gotToken != "signed-token" {
		t.Errorf("download token = %q, want signed-token (signed URL must be used verbatim)", gotToken)
	}
	if gotContent != gcodeBody {
		t.Errorf("uploaded content = %q, want the gcode bytes", gotContent)
	}
	if gotFilename != "benchy.gcode" {
		t.Errorf("uploaded filename = %q, want benchy.gcode", gotFilename)
	}
	if gotRoot != "gcodes" {
		t.Errorf("root field = %q, want gcodes", gotRoot)
	}
	if gotPrint != "true" {
		t.Errorf("print field = %q, want true (autostart)", gotPrint)
	}
	if result["print_started"] != true {
		t.Errorf("result.print_started = %v, want true", result["print_started"])
	}
}

// TestExecuteUploadFile_SSRFGuard rejects a download_url whose host doesn't match
// the configured cloud, without touching Moonraker.
func TestExecuteUploadFile_SSRFGuard(t *testing.T) {
	uploadHit := false
	mux := http.NewServeMux()
	mux.HandleFunc("/server/files/upload", func(w http.ResponseWriter, _ *http.Request) {
		uploadHit = true
		_, _ = io.WriteString(w, `{}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := &Agent{
		log:   discardLogger(),
		cfg:   &config.Config{ConnectorID: "1", CloudURL: "https://www.spoolr.io"},
		cloud: cloud.New(cloud.Options{BaseURL: "https://www.spoolr.io", ConnectorID: "1", Logger: discardLogger()}),
	}
	mc := moonraker.New(srv.URL, 80)

	cmd := cloud.Command{
		ID:        "9101",
		PrinterID: 1,
		Action:    "upload_file",
		Params: map[string]any{
			"filename":     "benchy.gcode",
			"download_url": "http://169.254.169.254/latest/meta-data/", // not the cloud host
			"print":        false,
		},
	}

	if err := a.executeUploadFile(context.Background(), mc, cmd, map[string]any{}); err == nil {
		t.Fatal("expected SSRF guard to reject a foreign download_url, got nil")
	}
	if uploadHit {
		t.Error("moonraker upload should never be called when the SSRF guard trips")
	}
}

// TestExecuteUploadFile_LegacyBase64 ensures the pre-existing base64 `content`
// path (the web-UI upload) still works after adding the download_url branch.
func TestExecuteUploadFile_LegacyBase64(t *testing.T) {
	const gcodeBody = "G28\nG1 X1 Y1\n"

	var (
		mu         sync.Mutex
		gotContent string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/server/files/upload", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(8 << 20)
		f, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer f.Close()
		body, _ := io.ReadAll(f)
		mu.Lock()
		gotContent = string(body)
		mu.Unlock()
		_, _ = io.WriteString(w, `{}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := &Agent{log: discardLogger(), cfg: &config.Config{ConnectorID: "1"}}
	mc := moonraker.New(srv.URL, 80)

	cmd := cloud.Command{
		ID:        "9102",
		PrinterID: 1,
		Action:    "upload_file",
		Params: map[string]any{
			"filename": "legacy.gcode",
			"content":  base64.StdEncoding.EncodeToString([]byte(gcodeBody)),
		},
	}

	if err := a.executeUploadFile(context.Background(), mc, cmd, map[string]any{}); err != nil {
		t.Fatalf("executeUploadFile (legacy): %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotContent != gcodeBody {
		t.Errorf("legacy uploaded content = %q, want %q", gotContent, gcodeBody)
	}
}
