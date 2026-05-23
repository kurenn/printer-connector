package agent

import (
	"log/slog"
	"testing"

	"printer-connector/internal/bambu"
	"printer-connector/internal/config"
	"printer-connector/internal/moonraker"
)

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
