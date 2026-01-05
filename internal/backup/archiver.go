package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Options configures backup archive creation
type Options struct {
	PrinterDataRoot string // e.g., "/home/pi/printer_data"
	IncludeConfig   bool
	IncludeDatabase bool
	IncludeGcodes   bool
	IncludeLogs     bool
	OutputPath      string // temp file path for archive
	MaxSizeBytes    int64  // safety limit (0 = no limit)
}

// Result contains metadata about the created backup archive
type Result struct {
	ArchivePath string
	SizeBytes   int64
	SHA256      string
}

// Create builds a tar.gz archive of selected printer_data directories
// and returns metadata including SHA256 hash.
func Create(opts Options) (*Result, error) {
	// Validate printer_data root exists
	if opts.PrinterDataRoot == "" {
		return nil, fmt.Errorf("printer_data_root is required")
	}

	cleanRoot := filepath.Clean(opts.PrinterDataRoot)
	if _, err := os.Stat(cleanRoot); err != nil {
		return nil, fmt.Errorf("printer_data_root does not exist: %w", err)
	}

	// Build list of directories to include
	dirs := []string{}
	if opts.IncludeConfig {
		dirs = append(dirs, "config")
	}
	if opts.IncludeDatabase {
		dirs = append(dirs, "database")
	}
	if opts.IncludeGcodes {
		dirs = append(dirs, "gcodes")
	}
	if opts.IncludeLogs {
		dirs = append(dirs, "logs")
	}

	if len(dirs) == 0 {
		return nil, fmt.Errorf("no directories selected for backup")
	}

	// Create output file
	outFile, err := os.Create(opts.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() {
		if outFile != nil {
			outFile.Close()
		}
	}()

	// Setup hash writer
	hasher := sha256.New()
	multiWriter := io.MultiWriter(outFile, hasher)

	// Create gzip writer
	gzWriter := gzip.NewWriter(multiWriter)
	defer gzWriter.Close()

	// Create tar writer with PAX format (supports long filenames)
	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	var totalSize int64

	// Add each directory to archive
	for _, dir := range dirs {
		dirPath := filepath.Join(cleanRoot, dir)

		// Check if directory exists (skip if not)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			continue
		}

		// Walk directory and add files
		err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Validate path is within printer_data root (security check)
			cleanPath := filepath.Clean(path)
			if !strings.HasPrefix(cleanPath, cleanRoot) {
				return fmt.Errorf("path outside printer_data root: %s", path)
			}

			// Skip Helper-Script directory
			if info.IsDir() && info.Name() == "Helper-Script" {
				return filepath.SkipDir
			}

			// Skip directories (we only archive files)
			if info.IsDir() {
				return nil
			}

			// Only include .cfg files
			if !strings.HasSuffix(info.Name(), ".cfg") {
				return nil
			}

			// Skip printer-*_*.cfg files (but keep printer.cfg)
			if strings.HasPrefix(info.Name(), "printer-") && strings.Contains(info.Name(), "_") && info.Name() != "printer.cfg" {
				return nil
			}

			// Check size limit
			if opts.MaxSizeBytes > 0 && totalSize+info.Size() > opts.MaxSizeBytes {
				return fmt.Errorf("archive size exceeds limit of %d bytes", opts.MaxSizeBytes)
			}

			// Calculate relative path for archive
			relPath, err := filepath.Rel(cleanRoot, path)
			if err != nil {
				return fmt.Errorf("failed to calculate relative path: %w", err)
			}
			// Use forward slashes for tar archives (Unix convention)
			relPath = filepath.ToSlash(relPath)

			// Create tar header with PAX format for long filenames
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return fmt.Errorf("failed to create tar header: %w", err)
			}
			header.Name = relPath
			header.Format = tar.FormatPAX // Use PAX format to support long filenames
			
			// Clear potentially long fields that might cause issues
			header.Uname = ""
			header.Gname = ""

			// Write header
			if err := tarWriter.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to write tar header: %w", err)
			}

			// Open and copy file contents
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open file %s: %w", path, err)
			}
			defer file.Close()

			written, err := io.Copy(tarWriter, file)
			if err != nil {
				return fmt.Errorf("failed to write file %s to archive: %w", path, err)
			}

			totalSize += written
			return nil
		})

		if err != nil {
			return nil, fmt.Errorf("failed to archive directory %s: %w", dir, err)
		}
	}

	// Close writers to flush buffers
	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close output file: %w", err)
	}
	outFile = nil // Prevent defer from closing again

	// Get final file size
	fileInfo, err := os.Stat(opts.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat output file: %w", err)
	}

	return &Result{
		ArchivePath: opts.OutputPath,
		SizeBytes:   fileInfo.Size(),
		SHA256:      fmt.Sprintf("%x", hasher.Sum(nil)),
	}, nil
}

// isWithinRoot checks if path is within root (security check)
func isWithinRoot(path, root string) bool {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	return strings.HasPrefix(cleanPath, cleanRoot)
}
