package agent

import (
	"log/slog"
	"testing"

	"printer-connector/internal/bambu"
	"printer-connector/internal/config"
	"printer-connector/internal/driver"
	"printer-connector/internal/moonraker"
)

func testAgent(t *testing.T, printers []config.Printer) *Agent {
	t.Helper()
	cfg := &config.Config{
		CloudURL:        "https://www.spoolr.io",
		ConnectorID:     "1",
		ConnectorSecret: "s",
		Printers:        printers,
	}
	return New(Options{Config: cfg, Logger: slog.Default(), Version: "test"})
}

func TestWithNormalized_AttachesCanonicalTelemetry(t *testing.T) {
	a := testAgent(t, []config.Printer{
		{PrinterID: 7, Type: config.TypeMoonraker, BaseURL: "http://localhost:7125"},
	})
	raw := map[string]any{"result": map[string]any{"status": map[string]any{
		"print_stats":    map[string]any{"state": "printing", "filename": "b.gcode"},
		"virtual_sdcard": map[string]any{"progress": 0.5},
	}}}

	out := a.withNormalized(7, raw)

	norm, ok := out["normalized"].(driver.Telemetry)
	if !ok {
		t.Fatalf("normalized not attached or wrong type: %T", out["normalized"])
	}
	if norm.State != driver.StatePrinting {
		t.Errorf("normalized state = %q, want printing", norm.State)
	}
	if out["result"] == nil {
		t.Error("raw payload must remain intact alongside normalized")
	}
}

func TestWithNormalized_NoDriverLeavesPayloadUntouched(t *testing.T) {
	a := testAgent(t, []config.Printer{
		{PrinterID: 7, Type: config.TypeMoonraker, BaseURL: "http://localhost:7125"},
	})
	out := a.withNormalized(999, map[string]any{"x": 1}) // no driver for id 999
	if _, ok := out["normalized"]; ok {
		t.Error("should not attach normalized when no driver exists for the printer")
	}
}

// New must build its driver map from Config.Printers (the canonical list Load
// populates), not the legacy Moonraker field which Load clears to nil.
func TestNew_BuildsDriversFromConfigPrinters(t *testing.T) {
	cfg := &config.Config{
		CloudURL:        "https://www.spoolr.io",
		ConnectorID:     "1",
		ConnectorSecret: "s",
		Printers: []config.Printer{
			{PrinterID: 7, Type: config.TypeMoonraker, BaseURL: "http://localhost:7125"},
			{PrinterID: 9, Type: config.TypeBambu, Host: "h", Serial: "s", AccessCode: "a"},
		},
	}
	a := New(Options{Config: cfg, Logger: slog.Default(), Version: "test"})
	if len(a.drivers) != 2 {
		t.Fatalf("expected 2 drivers, got %d", len(a.drivers))
	}
	if a.drivers[7] == nil || a.drivers[9] == nil {
		t.Fatalf("drivers not keyed by printer ID: %#v", a.drivers)
	}
}

func TestBuildDrivers_DispatchesOnType(t *testing.T) {
	drivers := buildDrivers([]config.Printer{
		{PrinterID: 1, Type: config.TypeMoonraker, BaseURL: "http://x:7125"},
		{PrinterID: 2, Type: config.TypeBambu, Host: "h", Serial: "s", AccessCode: "a"},
		{PrinterID: 3, Type: "", BaseURL: "http://y:7125"}, // empty type -> moonraker
	})
	if _, ok := drivers[1].(*moonraker.Client); !ok {
		t.Errorf("printer 1 should be a moonraker driver, got %T", drivers[1])
	}
	if _, ok := drivers[2].(*bambu.Client); !ok {
		t.Errorf("printer 2 should be a bambu driver, got %T", drivers[2])
	}
	if _, ok := drivers[3].(*moonraker.Client); !ok {
		t.Errorf("printer 3 (empty type) should default to moonraker, got %T", drivers[3])
	}
}
