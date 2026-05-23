package bambu

import (
	"context"
	"errors"
	"testing"

	"printer-connector/internal/driver"
)

// validCapabilities mirrors printer_capabilities.json's enum.
var validCapabilities = map[string]bool{
	"pause": true, "resume": true, "cancel": true, "start_print": true,
	"upload_file": true, "delete_file": true, "sync_files": true,
	"import_history": true, "create_backup": true, "homing": true,
	"remote_print": true, "webcam": true,
}

func TestBambu_TelemetryIsCanonical(t *testing.T) {
	c := New("01ABC", "12345678")
	got, err := c.Telemetry(context.Background())
	if err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	if got.SchemaVersion != driver.SchemaVersion {
		t.Errorf("schema_version = %d, want %d", got.SchemaVersion, driver.SchemaVersion)
	}
	if got.Driver != driver.Bambu {
		t.Errorf("driver = %q, want bambu", got.Driver)
	}
	if got.State != driver.StateOffline {
		t.Errorf("state = %q, want offline", got.State)
	}
}

func TestBambu_CapabilitiesAreValid(t *testing.T) {
	caps := New("s", "a").Capabilities()
	if len(caps) == 0 {
		t.Fatal("expected non-empty capabilities")
	}
	for _, cap := range caps {
		if !validCapabilities[cap] {
			t.Errorf("capability %q not in the canonical set", cap)
		}
	}
}

func TestBambu_ControlActionsNotImplemented(t *testing.T) {
	c := New("s", "a")
	if err := c.Pause(context.Background()); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Pause err = %v, want ErrNotImplemented", err)
	}
	if err := c.StartPrint(context.Background(), "x.gcode"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("StartPrint err = %v, want ErrNotImplemented", err)
	}
}
