package moonraker

import (
	"bytes"
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
	if got.Job.ElapsedS == nil || *got.Job.ElapsedS != 1260 {
		t.Errorf("elapsed_s = %v, want 1260", got.Job.ElapsedS)
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
	cases := []struct {
		name             string
		state            string
		isActive, paused bool
		want             string
	}{
		{"printing by state", "printing", false, false, driver.StatePrinting},
		{"printing by is_active flag", "standby", true, false, driver.StatePrinting},
		{"paused by state", "paused", false, false, driver.StatePaused},
		{"paused by is_paused flag", "printing", true, true, driver.StatePaused}, // paused wins over printing
		{"complete", "complete", false, false, driver.StateComplete},
		{"error wins over everything", "error", true, true, driver.StateError},
		{"standby", "standby", false, false, driver.StateIdle},
		{"cancelled", "cancelled", false, false, driver.StateIdle},
		{"empty", "", false, false, driver.StateIdle},
		{"unknown", "weird", false, false, driver.StateIdle},
	}
	for _, c := range cases {
		if got := moonrakerState(c.state, c.isActive, c.paused); got != c.want {
			t.Errorf("%s: moonrakerState(%q, active=%v, paused=%v) = %q, want %q",
				c.name, c.state, c.isActive, c.paused, got, c.want)
		}
	}
}

// Parity guard: a snapshot that the raw Printers::Status would call "printing"
// via virtual_sdcard.is_active (even with print_stats.state still "standby")
// must normalize to printing, not idle — otherwise emitting normalized would
// regress live status.
func TestNormalize_HonorsVirtualSdcardActive(t *testing.T) {
	raw := decodeRaw(t, `{"result":{"status":{
	  "print_stats":{"state":"standby","filename":"a.gcode"},
	  "virtual_sdcard":{"is_active":true,"progress":0.1}
	}}}`)
	if got := Normalize(raw).State; got != driver.StatePrinting {
		t.Errorf("state = %q, want printing (is_active true)", got)
	}
}

func TestNormalize_HonorsPauseResume(t *testing.T) {
	raw := decodeRaw(t, `{"result":{"status":{
	  "print_stats":{"state":"printing","filename":"a.gcode"},
	  "pause_resume":{"is_paused":true}
	}}}`)
	if got := Normalize(raw).State; got != driver.StatePaused {
		t.Errorf("state = %q, want paused (is_paused true)", got)
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

// A printer that reports no print_duration must yield a nil elapsed rather than
// a zero, so the cloud can tell "not reported" from "zero seconds in".
func TestNormalizeOmitsElapsedWhenNotReported(t *testing.T) {
	raw := map[string]any{"result": map[string]any{"status": map[string]any{
		"print_stats": map[string]any{"state": "standby", "filename": ""},
	}}}
	got := Normalize(raw)
	if got.Job == nil {
		t.Fatal("Job = nil")
	}
	if got.Job.ElapsedS != nil {
		t.Errorf("elapsed_s = %v, want nil when print_duration is absent", *got.Job.ElapsedS)
	}
}

// elapsed_s must be omitted from the wire when unknown — the cloud reads a
// missing key as "unknown", but a present 0 as a real reading.
func TestElapsedOmittedFromJSONWhenNil(t *testing.T) {
	raw := map[string]any{"result": map[string]any{"status": map[string]any{
		"print_stats": map[string]any{"state": "standby"},
	}}}
	b, err := json.Marshal(Normalize(raw))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte("elapsed_s")) {
		t.Errorf("elapsed_s should be absent from JSON when unknown, got: %s", b)
	}
}
