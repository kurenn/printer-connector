// Package gcode fetches the G-code content of a printer's active print from
// Moonraker so the cloud can render a live 3D toolpath preview. It mirrors the
// backup fetch flow: pull over Moonraker's file API (the connector need not run
// on the printer), stream to a temp file (G-code can be tens of MB — never
// buffered in memory), then the caller uploads it to a presigned cloud URL.
package gcode

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// MoonrakerRoot is the Moonraker file-manager root that holds sliced G-code.
const MoonrakerRoot = "gcodes"

// Downloader streams a file from a Moonraker root into w. *moonraker.Client
// satisfies this via DownloadFileStream.
type Downloader interface {
	DownloadFileStream(ctx context.Context, root, filePath string, w io.Writer) error
}

// Result describes a fetched G-code file staged on local disk.
type Result struct {
	Path      string // temp file path the caller uploads, then removes
	SizeBytes int64
}

// FetchToTemp downloads the named G-code file from Moonraker's "gcodes" root
// into a fresh temp file and returns its path + size. The caller owns the temp
// file and must remove it.
//
// SECURITY: filename comes from the trusted print_stats snapshot (relayed by
// the cloud), never raw user input. We still reject absolute paths and any
// "../" traversal so a malformed/hostile value can't escape the gcodes root.
func FetchToTemp(ctx context.Context, d Downloader, filename string) (*Result, error) {
	clean, err := sanitizeFilename(filename)
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp("", "spoolr-gcode-*.gcode")
	if err != nil {
		return nil, fmt.Errorf("create temp gcode file: %w", err)
	}
	tmpPath := tmp.Name()

	// On any failure remove the partial temp file so we don't leak it.
	success := false
	defer func() {
		if !success {
			tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if err := d.DownloadFileStream(ctx, MoonrakerRoot, clean, tmp); err != nil {
		return nil, fmt.Errorf("download gcode %q: %w", clean, err)
	}

	info, err := tmp.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat fetched gcode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close fetched gcode: %w", err)
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("fetched gcode %q is empty", clean)
	}

	success = true
	return &Result{Path: tmpPath, SizeBytes: info.Size()}, nil
}

// sanitizeFilename validates a Moonraker "gcodes"-root relative path. Moonraker
// stores files under the gcodes root (optionally in subdirectories), so a
// forward-slash relative path is allowed, but absolute paths and ".." segments
// are not.
func sanitizeFilename(filename string) (string, error) {
	name := strings.TrimSpace(filename)
	if name == "" {
		return "", fmt.Errorf("empty gcode filename")
	}
	if strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("absolute gcode path not allowed: %q", filename)
	}
	// path.Clean collapses any traversal; if cleaning changes a leading segment
	// to ".." the path tried to escape the root.
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("gcode path escapes root: %q", filename)
	}
	return clean, nil
}
