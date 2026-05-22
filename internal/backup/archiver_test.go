package backup

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// setupPrinterData builds a representative printer_data tree and returns its root.
func setupPrinterData(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("config/printer.cfg", "[printer]\n")
	write("config/macros.cfg", "[gcode_macro X]\n")
	write("config/printer-20240101_120000.cfg", "generated variant\n") // excluded
	write("config/notes.txt", "not a cfg\n")                           // excluded
	write("database/data.mdb", "BINARY-LMDB-CONTENT")
	write("gcodes/benchy.gcode", "G28\n")
	write("logs/klippy.log", "started\n")
	return root
}

// archiveEntries returns the set of file paths contained in a tar.gz archive.
func archiveEntries(t *testing.T, path string) map[string]bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	entries := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		entries[h.Name] = true
	}
	return entries
}

func runCreate(t *testing.T, opts Options) (*Result, error) {
	t.Helper()
	if opts.OutputPath == "" {
		opts.OutputPath = filepath.Join(t.TempDir(), "out.tar.gz")
	}
	return Create(opts)
}

func TestCreate_ConfigOnly_FiltersCfgVariantsAndNonCfg(t *testing.T) {
	root := setupPrinterData(t)
	res, err := runCreate(t, Options{PrinterDataRoot: root, IncludeConfig: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := archiveEntries(t, res.ArchivePath)

	want := []string{"config/printer.cfg", "config/macros.cfg"}
	for _, w := range want {
		if !got[w] {
			t.Errorf("expected %q in archive, entries=%v", w, got)
		}
	}
	excluded := []string{"config/printer-20240101_120000.cfg", "config/notes.txt", "database/data.mdb"}
	for _, e := range excluded {
		if got[e] {
			t.Errorf("did not expect %q in archive", e)
		}
	}
}

// Regression test: include_database is requested by default but the archiver
// previously dropped everything except .cfg files, silently losing the DB.
func TestCreate_DatabaseIncluded(t *testing.T) {
	root := setupPrinterData(t)
	res, err := runCreate(t, Options{PrinterDataRoot: root, IncludeDatabase: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := archiveEntries(t, res.ArchivePath)
	if !got["database/data.mdb"] {
		t.Fatalf("expected database/data.mdb in archive, entries=%v", got)
	}
}

func TestCreate_GcodesAndLogsIncluded(t *testing.T) {
	root := setupPrinterData(t)
	res, err := runCreate(t, Options{PrinterDataRoot: root, IncludeGcodes: true, IncludeLogs: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := archiveEntries(t, res.ArchivePath)
	for _, w := range []string{"gcodes/benchy.gcode", "logs/klippy.log"} {
		if !got[w] {
			t.Errorf("expected %q in archive, entries=%v", w, got)
		}
	}
}

func TestCreate_NoIncludesIsError(t *testing.T) {
	root := setupPrinterData(t)
	if _, err := runCreate(t, Options{PrinterDataRoot: root}); err == nil {
		t.Fatal("expected error when no directories selected")
	}
}

func TestCreate_SizeLimitEnforced(t *testing.T) {
	root := setupPrinterData(t)
	_, err := runCreate(t, Options{PrinterDataRoot: root, IncludeConfig: true, MaxSizeBytes: 1})
	if err == nil {
		t.Fatal("expected size-limit error")
	}
}

func TestCreate_MissingRootIsError(t *testing.T) {
	_, err := runCreate(t, Options{PrinterDataRoot: filepath.Join(t.TempDir(), "does-not-exist"), IncludeConfig: true})
	if err == nil {
		t.Fatal("expected error for missing printer_data root")
	}
}

func TestCreate_PopulatesResultMetadata(t *testing.T) {
	root := setupPrinterData(t)
	res, err := runCreate(t, Options{PrinterDataRoot: root, IncludeConfig: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.SizeBytes <= 0 {
		t.Errorf("expected positive SizeBytes, got %d", res.SizeBytes)
	}
	if len(res.SHA256) != 64 {
		t.Errorf("expected 64-char sha256, got %q", res.SHA256)
	}
}

func TestIsWithinRoot_SeparatorBoundary(t *testing.T) {
	cases := []struct {
		path, root string
		want       bool
	}{
		{"/data/printer_data", "/data/printer_data", true},
		{"/data/printer_data/config/printer.cfg", "/data/printer_data", true},
		{"/data/printer_database", "/data/printer_data", false}, // prefix without boundary
		{"/etc/passwd", "/data/printer_data", false},
	}
	for _, c := range cases {
		if got := isWithinRoot(c.path, c.root); got != c.want {
			t.Errorf("isWithinRoot(%q, %q) = %v, want %v", c.path, c.root, got, c.want)
		}
	}
}
