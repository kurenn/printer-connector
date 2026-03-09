package agent

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"printer-connector/internal/cloud"
	"printer-connector/internal/moonraker"
)

func TestExecuteRemotePrintSuccess(t *testing.T) {
	t.Parallel()

	gcode := []byte("G28\nG1 X10 Y10\n")
	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/benchy.gcode" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(gcode)
	}))
	defer download.Close()

	var uploadedFilename string
	var uploadedContent []byte
	var startFilename string

	moon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/server/files/upload":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parse multipart: %v", err)
			}
			f, hdr, err := r.FormFile("file")
			if err != nil {
				t.Fatalf("missing file form part: %v", err)
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil {
				t.Fatalf("read form file: %v", err)
			}
			uploadedFilename = hdr.Filename
			uploadedContent = data
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":"ok"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/printer/print/start":
			startFilename = r.URL.Query().Get("filename")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":"ok"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer moon.Close()

	a := &Agent{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mc := moonraker.New(moon.URL, 80)

	cmd := cloud.Command{Params: map[string]any{
		"download_url": download.URL + "/jobs/benchy.gcode",
	}}
	result := map[string]any{}

	err := a.executeRemotePrint(context.Background(), mc, cmd, result)
	if err != nil {
		t.Fatalf("executeRemotePrint returned error: %v", err)
	}

	if uploadedFilename != "benchy.gcode" {
		t.Fatalf("expected uploaded filename benchy.gcode, got %q", uploadedFilename)
	}
	if string(uploadedContent) != string(gcode) {
		t.Fatalf("uploaded content mismatch")
	}
	if startFilename != "benchy.gcode" {
		t.Fatalf("expected start filename benchy.gcode, got %q", startFilename)
	}
	if got, ok := result["started"].(bool); !ok || !got {
		t.Fatalf("expected started=true in result, got %#v", result["started"])
	}
}

func TestExecuteRemotePrintShaMismatch(t *testing.T) {
	t.Parallel()

	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("G28\n"))
	}))
	defer download.Close()

	moon := httptest.NewServer(http.NotFoundHandler())
	defer moon.Close()

	a := &Agent{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mc := moonraker.New(moon.URL, 80)

	cmd := cloud.Command{Params: map[string]any{
		"download_url": download.URL + "/part.gcode",
		"sha256":       strings.Repeat("0", 64),
	}}
	result := map[string]any{}

	err := a.executeRemotePrint(context.Background(), mc, cmd, result)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteRemotePrintRejectsInvalidExtension(t *testing.T) {
	t.Parallel()

	moon := httptest.NewServer(http.NotFoundHandler())
	defer moon.Close()

	a := &Agent{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mc := moonraker.New(moon.URL, 80)

	cmd := cloud.Command{Params: map[string]any{
		"download_url": "https://files.example.com/builds/file.txt",
	}}
	result := map[string]any{}

	err := a.executeRemotePrint(context.Background(), mc, cmd, result)
	if err == nil {
		t.Fatal("expected invalid filename extension error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid params.filename") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteRemotePrintRejectsPathSeparatorsInFilename(t *testing.T) {
	t.Parallel()

	moon := httptest.NewServer(http.NotFoundHandler())
	defer moon.Close()

	a := &Agent{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mc := moonraker.New(moon.URL, 80)

	cmd := cloud.Command{Params: map[string]any{
		"download_url": "https://files.example.com/builds/file.gcode",
		"filename":     "../unsafe.gcode",
	}}
	result := map[string]any{}

	err := a.executeRemotePrint(context.Background(), mc, cmd, result)
	if err == nil {
		t.Fatal("expected path separator validation error, got nil")
	}
	if !strings.Contains(err.Error(), "path separators") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteRemotePrintNoAutoStart(t *testing.T) {
	t.Parallel()

	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("G28\n"))
	}))
	defer download.Close()

	startCalled := false
	moon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/server/files/upload":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":"ok"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/printer/print/start":
			startCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":"ok"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer moon.Close()

	a := &Agent{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mc := moonraker.New(moon.URL, 80)

	cmd := cloud.Command{Params: map[string]any{
		"download_url": download.URL + "/part.gcode",
		"filename":     "part.gcode",
		"start_print":  false,
	}}
	result := map[string]any{}

	err := a.executeRemotePrint(context.Background(), mc, cmd, result)
	if err != nil {
		t.Fatalf("executeRemotePrint returned error: %v", err)
	}
	if startCalled {
		t.Fatal("start print endpoint should not be called when start_print=false")
	}
	if got, ok := result["started"].(bool); !ok || got {
		t.Fatalf("expected started=false in result, got %#v", result["started"])
	}
}
