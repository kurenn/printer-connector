package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// RemoteFile describes a file available from a remote printer.
type RemoteFile struct {
	Path string
	Size int64
}

// Fetcher pulls printer files over the network (e.g. Moonraker's file API), so
// backups do not require the connector to run on the printer itself.
type Fetcher interface {
	ListFiles(ctx context.Context, root string) ([]RemoteFile, error)
	DownloadFile(ctx context.Context, root, path string, w io.Writer) error
}

// FetchAndCreate stages the requested file roots (config/logs/gcodes) from a
// remote printer into a temp directory, then archives them with Create. The
// connector talks to the printer over the network, so it need not run on it.
//
// The Moonraker database is not a file-manager root, so include_database is not
// fetched here — that needs the separate Moonraker DB API (a follow-up).
func FetchAndCreate(ctx context.Context, f Fetcher, opts Options) (*Result, error) {
	roots := fetchRoots(opts)
	if len(roots) == 0 {
		return nil, fmt.Errorf("no fetchable directories selected (config/logs/gcodes)")
	}

	staging, err := os.MkdirTemp("", "spoolr-backup-stage-*")
	if err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(staging)

	var total int64
	for _, root := range roots {
		files, err := f.ListFiles(ctx, root)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", root, err)
		}
		rootDir := filepath.Join(staging, root)
		for _, file := range files {
			dest := filepath.Join(rootDir, filepath.FromSlash(file.Path))
			// Guard against path traversal from a hostile or buggy file listing.
			if !isWithinRoot(dest, rootDir) {
				return nil, fmt.Errorf("unsafe path in %s listing: %q", root, file.Path)
			}
			if opts.MaxSizeBytes > 0 {
				total += file.Size
				if total > opts.MaxSizeBytes {
					return nil, fmt.Errorf("backup exceeds size limit of %d bytes", opts.MaxSizeBytes)
				}
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return nil, fmt.Errorf("stage dir for %s: %w", file.Path, err)
			}
			out, err := os.Create(dest)
			if err != nil {
				return nil, fmt.Errorf("create staged %s: %w", file.Path, err)
			}
			dlErr := f.DownloadFile(ctx, root, file.Path, out)
			closeErr := out.Close()
			if dlErr != nil {
				return nil, fmt.Errorf("download %s/%s: %w", root, file.Path, dlErr)
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
	}

	// Reuse the local archiver on the staged tree. The Moonraker DB isn't a file
	// root, so never try to archive a (nonexistent) staged database dir.
	createOpts := opts
	createOpts.PrinterDataRoot = staging
	createOpts.IncludeDatabase = false
	return Create(createOpts)
}

// fetchRoots maps include flags to Moonraker file-manager roots.
func fetchRoots(opts Options) []string {
	roots := make([]string, 0, 3)
	if opts.IncludeConfig {
		roots = append(roots, "config")
	}
	if opts.IncludeLogs {
		roots = append(roots, "logs")
	}
	if opts.IncludeGcodes {
		roots = append(roots, "gcodes")
	}
	return roots
}
