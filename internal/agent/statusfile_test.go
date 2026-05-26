package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"printer-connector/internal/config"
	"printer-connector/internal/driver"
)

// readStatusFile is a helper that reads and parses the status.json written to dir.
func readStatusFile(t *testing.T, dir string) statusFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil {
		t.Fatalf("status.json not found: %v", err)
	}
	var sf statusFile
	if err := json.Unmarshal(b, &sf); err != nil {
		t.Fatalf("status.json invalid JSON: %v", err)
	}
	return sf
}

func TestWriteStatusFile_ValidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "connector.json")
	cfg := &config.Config{SiteName: "lab"}

	entries := []printerStatus{
		{PrinterID: 1, Kind: "klipper", Reachable: true, State: "idle"},
	}
	if err := writeStatusFile(cfgPath, cfg, entries); err != nil {
		t.Fatalf("writeStatusFile: %v", err)
	}

	// Must be readable as JSON.
	b, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil {
		t.Fatalf("status.json missing: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("status.json is not valid JSON: %v", err)
	}
}

func TestWriteStatusFile_SchemaVersionAndUpdatedAt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "connector.json")
	cfg := &config.Config{}

	before := time.Now().UTC().Add(-time.Second)
	if err := writeStatusFile(cfgPath, cfg, nil); err != nil {
		t.Fatalf("writeStatusFile: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)

	sf := readStatusFile(t, dir)

	if sf.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", sf.SchemaVersion)
	}
	ts, err := time.Parse(time.RFC3339, sf.UpdatedAt)
	if err != nil {
		t.Fatalf("updated_at %q is not RFC3339: %v", sf.UpdatedAt, err)
	}
	if ts.Before(before) || ts.After(after) {
		t.Errorf("updated_at %v outside expected window [%v, %v]", ts, before, after)
	}
}

func TestWriteStatusFile_PrintingPrinter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "connector.json")
	cfg := &config.Config{SiteName: "farm"}

	prog := 0.42
	remaining := int64(1234)
	curLayer := 120
	totLayer := 300
	nozzle := 210.5
	bed := 60.0

	tel := driver.Telemetry{
		State: driver.StatePrinting,
		Job: &driver.Job{
			Filename:     "benchy.gcode",
			Progress:     prog,
			RemainingS:   &remaining,
			CurrentLayer: &curLayer,
			TotalLayers:  &totLayer,
		},
		Temps: &driver.Temps{
			Nozzle: &driver.Sensor{Actual: nozzle, Target: 215},
			Bed:    &driver.Sensor{Actual: bed, Target: 65},
		},
	}

	p := config.Printer{PrinterID: 1, Name: "K1Max-1814", Type: config.TypeMoonraker}
	entry := buildPrinterStatus(p, tel, true)

	if err := writeStatusFile(cfgPath, cfg, []printerStatus{entry}); err != nil {
		t.Fatalf("writeStatusFile: %v", err)
	}

	sf := readStatusFile(t, dir)

	if sf.SiteName != "farm" {
		t.Errorf("site_name = %q, want farm", sf.SiteName)
	}
	if len(sf.Printers) != 1 {
		t.Fatalf("printers length = %d, want 1", len(sf.Printers))
	}
	got := sf.Printers[0]

	if got.PrinterID != 1 {
		t.Errorf("printer_id = %d, want 1", got.PrinterID)
	}
	if got.Name != "K1Max-1814" {
		t.Errorf("name = %q, want K1Max-1814", got.Name)
	}
	if got.Kind != "klipper" {
		t.Errorf("kind = %q, want klipper", got.Kind)
	}
	if !got.Reachable {
		t.Error("reachable = false, want true")
	}
	if got.State != driver.StatePrinting {
		t.Errorf("state = %q, want printing", got.State)
	}
	if got.Progress == nil || *got.Progress != prog {
		t.Errorf("progress = %v, want %v", got.Progress, prog)
	}
	if got.Job != "benchy.gcode" {
		t.Errorf("job = %q, want benchy.gcode", got.Job)
	}
	if got.RemainingS == nil || *got.RemainingS != remaining {
		t.Errorf("remaining_s = %v, want %d", got.RemainingS, remaining)
	}
	if got.LayerCurrent == nil || *got.LayerCurrent != curLayer {
		t.Errorf("layer_current = %v, want %d", got.LayerCurrent, curLayer)
	}
	if got.LayerTotal == nil || *got.LayerTotal != totLayer {
		t.Errorf("layer_total = %v, want %d", got.LayerTotal, totLayer)
	}
	if got.NozzleC == nil || *got.NozzleC != nozzle {
		t.Errorf("nozzle_c = %v, want %v", got.NozzleC, nozzle)
	}
	if got.BedC == nil || *got.BedC != bed {
		t.Errorf("bed_c = %v, want %v", got.BedC, bed)
	}
}

func TestWriteStatusFile_IdlePrinter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "connector.json")
	cfg := &config.Config{}

	tel := driver.Telemetry{State: driver.StateIdle}
	p := config.Printer{PrinterID: 2, Name: "Ender", Type: config.TypeMoonraker}
	entry := buildPrinterStatus(p, tel, true)

	if err := writeStatusFile(cfgPath, cfg, []printerStatus{entry}); err != nil {
		t.Fatalf("writeStatusFile: %v", err)
	}

	sf := readStatusFile(t, dir)
	if len(sf.Printers) != 1 {
		t.Fatalf("printers length = %d, want 1", len(sf.Printers))
	}
	got := sf.Printers[0]

	if got.State != driver.StateIdle {
		t.Errorf("state = %q, want idle", got.State)
	}
	if got.Progress != nil {
		t.Errorf("progress should be omitted for idle printer, got %v", *got.Progress)
	}
	if got.NozzleC != nil {
		t.Errorf("nozzle_c should be omitted for idle printer, got %v", *got.NozzleC)
	}
}

func TestWriteStatusFile_UnreachablePrinter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "connector.json")
	cfg := &config.Config{}

	p := config.Printer{PrinterID: 3, Name: "Bambu P1S", Type: config.TypeBambu}
	entry := buildPrinterStatus(p, driver.Telemetry{}, false)

	if err := writeStatusFile(cfgPath, cfg, []printerStatus{entry}); err != nil {
		t.Fatalf("writeStatusFile: %v", err)
	}

	sf := readStatusFile(t, dir)
	if len(sf.Printers) != 1 {
		t.Fatalf("printers length = %d, want 1", len(sf.Printers))
	}
	got := sf.Printers[0]

	if got.Reachable {
		t.Error("reachable = true, want false for unreachable printer")
	}
	if got.State != driver.StateOffline {
		t.Errorf("state = %q, want offline for unreachable printer", got.State)
	}
	if got.Kind != "bambu" {
		t.Errorf("kind = %q, want bambu", got.Kind)
	}
	if got.Progress != nil {
		t.Errorf("progress should be absent for unreachable printer, got %v", *got.Progress)
	}
	if got.NozzleC != nil {
		t.Errorf("nozzle_c should be absent for unreachable printer, got %v", *got.NozzleC)
	}
}

func TestWriteStatusFile_MultiPrinterMixed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "connector.json")
	cfg := &config.Config{SiteName: "workshop"}

	prog := 0.75
	nozzle := 200.0
	tel := driver.Telemetry{
		State: driver.StatePrinting,
		Job:   &driver.Job{Filename: "cube.gcode", Progress: prog},
		Temps: &driver.Temps{Nozzle: &driver.Sensor{Actual: nozzle}},
	}

	entries := []printerStatus{
		buildPrinterStatus(config.Printer{PrinterID: 1, Name: "Printer1", Type: config.TypeMoonraker}, tel, true),
		buildPrinterStatus(config.Printer{PrinterID: 2, Name: "Printer2", Type: config.TypeBambu}, driver.Telemetry{}, false),
	}

	if err := writeStatusFile(cfgPath, cfg, entries); err != nil {
		t.Fatalf("writeStatusFile: %v", err)
	}

	sf := readStatusFile(t, dir)
	if len(sf.Printers) != 2 {
		t.Fatalf("printers length = %d, want 2", len(sf.Printers))
	}

	// First printer: printing
	p1 := sf.Printers[0]
	if !p1.Reachable || p1.State != driver.StatePrinting {
		t.Errorf("printer 1: reachable=%v state=%q, want reachable=true state=printing", p1.Reachable, p1.State)
	}

	// Second printer: unreachable/offline
	p2 := sf.Printers[1]
	if p2.Reachable || p2.State != driver.StateOffline {
		t.Errorf("printer 2: reachable=%v state=%q, want reachable=false state=offline", p2.Reachable, p2.State)
	}
}

func TestPrinterKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		driverType string
		want       string
	}{
		{config.TypeMoonraker, "klipper"},
		{"", "klipper"},
		{config.TypeBambu, "bambu"},
		{"prusalink", "printer"},
		{"sdcp", "printer"},
	}
	for _, tc := range cases {
		if got := printerKind(tc.driverType); got != tc.want {
			t.Errorf("printerKind(%q) = %q, want %q", tc.driverType, got, tc.want)
		}
	}
}

func TestWriteStatusFile_AtomicWriteNoTmpRemaining(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "connector.json")
	cfg := &config.Config{}

	if err := writeStatusFile(cfgPath, cfg, nil); err != nil {
		t.Fatalf("writeStatusFile: %v", err)
	}

	// The temp file must be cleaned up after the atomic rename.
	if _, err := os.Stat(filepath.Join(dir, "status.json.tmp")); !os.IsNotExist(err) {
		t.Error("status.json.tmp should not remain after a successful write")
	}
	// And the final file must exist.
	if _, err := os.Stat(filepath.Join(dir, "status.json")); err != nil {
		t.Errorf("status.json missing after write: %v", err)
	}
}
