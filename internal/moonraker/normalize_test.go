package moonraker

import (
	"encoding/json"
	"testing"

	"printer-connector/internal/driver"
)

// canonicalStates mirrors printer_telemetry.json's `state` enum.
var canonicalStates = map[string]bool{
	driver.StateIdle: true, driver.StatePrinting: true, driver.StatePaused: true,
	driver.StateError: true, driver.StateOffline: true, driver.StateComplete: true,
}

// decodeRaw parses JSON the way QueryObjects does, so numbers arrive as float64.
func decodeRaw(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNormalize_PrintingSnapshot(t *testing.T) {
	raw := decodeRaw(t, `{
	  "result": { "status": {
	    "print_stats": {
	      "state": "printing",
	      "filename": "benchy.gcode",
	      "print_duration": 1260.0,
	      "info": { "current_layer": 84, "total_layer": 200 }
	    },
	    "virtual_sdcard": { "progress": 0.42 },
	    "extruder":   { "temperature": 219.4, "target": 220.0 },
	    "heater_bed": { "temperature": 60.1,  "target": 60.0 }
	  }}
	}`)

	got := Normalize(raw)

	// Conformance with the canonical schema.
	if got.SchemaVersion != driver.SchemaVersion {
		t.Errorf("schema_version = %d, want %d", got.SchemaVersion, driver.SchemaVersion)
	}
	if got.Driver != driver.Moonraker {
		t.Errorf("driver = %q, want %q", got.Driver, driver.Moonraker)
	}
	if !canonicalStates[got.State] {
		t.Errorf("state %q is not a canonical state", got.State)
	}

	if got.State != driver.StatePrinting {
		t.Errorf("state = %q, want printing", got.State)
	}
	if got.Job == nil {
		t.Fatal("expected job telemetry")
	}
	if got.Job.Filename != "benchy.gcode" {
		t.Errorf("filename = %q", got.Job.Filename)
	}
	if got.Job.Progress != 0.42 {
		t.Errorf("progress = %v, want 0.42", got.Job.Progress)
	}
	if got.Job.ElapsedS != 1260 {
		t.Errorf("elapsed_s = %d, want 1260", got.Job.ElapsedS)
	}
	if got.Job.CurrentLayer == nil || *got.Job.CurrentLayer != 84 {
		t.Errorf("current_layer = %v, want 84", got.Job.CurrentLayer)
	}
	if got.Job.TotalLayers == nil || *got.Job.TotalLayers != 200 {
		t.Errorf("total_layers = %v, want 200", got.Job.TotalLayers)
	}
	if got.Temps == nil || got.Temps.Nozzle == nil || got.Temps.Bed == nil {
		t.Fatal("expected nozzle and bed temps")
	}
	if got.Temps.Nozzle.Actual != 219.4 || got.Temps.Nozzle.Target != 220.0 {
		t.Errorf("nozzle = %+v", *got.Temps.Nozzle)
	}
	if got.Temps.Bed.Actual != 60.1 || got.Temps.Bed.Target != 60.0 {
		t.Errorf("bed = %+v", *got.Temps.Bed)
	}
}

func TestNormalize_StateMapping(t *testing.T) {
	cases := map[string]string{
		"printing":  driver.StatePrinting,
		"paused":    driver.StatePaused,
		"complete":  driver.StateComplete,
		"error":     driver.StateError,
		"standby":   driver.StateIdle,
		"cancelled": driver.StateIdle,
		"":          driver.StateIdle,
		"weird":     driver.StateIdle,
	}
	for in, want := range cases {
		if got := mapState(in); got != want {
			t.Errorf("mapState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalize_EmptyIsSafe(t *testing.T) {
	got := Normalize(map[string]any{})
	if got.SchemaVersion != driver.SchemaVersion || got.Driver != driver.Moonraker {
		t.Errorf("missing canonical envelope: %+v", got)
	}
	if !canonicalStates[got.State] {
		t.Errorf("state %q is not canonical", got.State)
	}
	if got.Temps != nil {
		t.Errorf("expected no temps for empty input, got %+v", got.Temps)
	}
}
