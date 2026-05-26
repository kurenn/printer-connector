package moonraker

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRootFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/server/files/list" && r.URL.Query().Get("root") == "config" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":[{"path":"printer.cfg","size":12},{"path":"sub/foo.cfg","size":3}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(srv.URL, 0)
	files, err := c.ListRootFiles(context.Background(), "config")
	if err != nil {
		t.Fatalf("ListRootFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if files[0].Path != "printer.cfg" || files[0].Size != 12 {
		t.Errorf("unexpected file[0]: %+v", files[0])
	}
	if files[1].Path != "sub/foo.cfg" {
		t.Errorf("unexpected file[1]: %+v", files[1])
	}
}

func TestDownloadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/server/files/config/printer.cfg" {
			_, _ = w.Write([]byte("hello cfg"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(srv.URL, 0)
	var buf bytes.Buffer
	if err := c.DownloadFile(context.Background(), "config", "printer.cfg", &buf); err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if buf.String() != "hello cfg" {
		t.Errorf("downloaded %q, want %q", buf.String(), "hello cfg")
	}
}

func TestDownloadFileNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(srv.URL, 0)
	var buf bytes.Buffer
	if err := c.DownloadFile(context.Background(), "config", "missing.cfg", &buf); err == nil {
		t.Error("expected an error for a 404 download")
	}
}

func TestDownloadFileStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/server/files/gcodes/benchy.gcode" {
			_, _ = w.Write([]byte("G28\nG1 X1 Y1 E1\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(srv.URL, 0)
	var buf bytes.Buffer
	if err := c.DownloadFileStream(context.Background(), "gcodes", "benchy.gcode", &buf); err != nil {
		t.Fatalf("DownloadFileStream: %v", err)
	}
	if buf.String() != "G28\nG1 X1 Y1 E1\n" {
		t.Errorf("streamed %q, want gcode body", buf.String())
	}
}

func TestDownloadFileStreamNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(srv.URL, 0)
	var buf bytes.Buffer
	if err := c.DownloadFileStream(context.Background(), "gcodes", "missing.gcode", &buf); err == nil {
		t.Error("expected an error for a 404 stream download")
	}
}
