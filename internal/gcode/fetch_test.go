package gcode

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
)

// fakeDownloader records the requested (root, path) and writes canned bytes.
type fakeDownloader struct {
	gotRoot string
	gotPath string
	data    []byte
	err     error
}

func (f *fakeDownloader) DownloadFileStream(_ context.Context, root, filePath string, w io.Writer) error {
	f.gotRoot = root
	f.gotPath = filePath
	if f.err != nil {
		return f.err
	}
	_, err := w.Write(f.data)
	return err
}

func TestFetchToTemp_WritesTempFileFromGcodesRoot(t *testing.T) {
	body := []byte("G28\nG1 Z0.2 F300\nG1 X10 Y10 E1\n")
	d := &fakeDownloader{data: body}

	res, err := FetchToTemp(context.Background(), d, "benchy.gcode")
	if err != nil {
		t.Fatalf("FetchToTemp: %v", err)
	}
	defer os.Remove(res.Path)

	if d.gotRoot != "gcodes" {
		t.Errorf("downloaded from root %q, want %q", d.gotRoot, "gcodes")
	}
	if d.gotPath != "benchy.gcode" {
		t.Errorf("downloaded path %q, want %q", d.gotPath, "benchy.gcode")
	}
	if res.SizeBytes != int64(len(body)) {
		t.Errorf("size %d, want %d", res.SizeBytes, len(body))
	}
	got, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("temp file = %q, want %q", got, body)
	}
}

func TestFetchToTemp_AllowsSubdirectoryPaths(t *testing.T) {
	d := &fakeDownloader{data: []byte("G1\n")}
	res, err := FetchToTemp(context.Background(), d, "projects/cube.gcode")
	if err != nil {
		t.Fatalf("FetchToTemp: %v", err)
	}
	defer os.Remove(res.Path)
	if d.gotPath != "projects/cube.gcode" {
		t.Errorf("path %q, want %q", d.gotPath, "projects/cube.gcode")
	}
}

func TestFetchToTemp_RejectsTraversalAndAbsolutePaths(t *testing.T) {
	for _, name := range []string{
		"",
		"   ",
		"/etc/passwd",
		"../../etc/passwd",
		"..",
	} {
		d := &fakeDownloader{data: []byte("x")}
		if _, err := FetchToTemp(context.Background(), d, name); err == nil {
			t.Errorf("expected error for unsafe filename %q, got nil", name)
		}
		if d.gotPath != "" {
			t.Errorf("download attempted for unsafe filename %q (path=%q)", name, d.gotPath)
		}
	}
}

func TestFetchToTemp_EmptyDownloadIsError_AndCleansUp(t *testing.T) {
	d := &fakeDownloader{data: []byte{}}
	res, err := FetchToTemp(context.Background(), d, "empty.gcode")
	if err == nil {
		os.Remove(res.Path)
		t.Fatalf("expected error for empty gcode, got nil")
	}
}

func TestFetchToTemp_DownloadErrorIsPropagated_AndCleansUp(t *testing.T) {
	d := &fakeDownloader{err: fmt.Errorf("boom")}
	if _, err := FetchToTemp(context.Background(), d, "benchy.gcode"); err == nil {
		t.Fatalf("expected error when download fails, got nil")
	}
}
