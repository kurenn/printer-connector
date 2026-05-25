package bambu

import (
	"testing"

	"printer-connector/internal/driver"
)

// runningReport is a representative LAN push_status report for a printing job,
// using the field names and units (mc_remaining_time in minutes, mc_percent in
// whole percent) the printer actually emits.
func runningReport() map[string]any {
	return map[string]any{
		"print": map[string]any{
			"gcode_state":          "RUNNING",
			"mc_percent":           42.0,
			"mc_remaining_time":    29.0,
			"layer_num":            84.0,
			"total_layer_num":      200.0,
			"nozzle_temper":        219.4,
			"nozzle_target_temper": 220.0,
			"bed_temper":           60.1,
			"bed_target_temper":    60.0,
			"chamber_temper":       38.0,
			"subtask_name":         "benchy",
			"gcode_file":           "benchy.3mf",
		},
	}
}

func TestNormalizeRunningJob(t *testing.T) {
	tel := Normalize(runningReport())

	if tel.SchemaVersion != driver.SchemaVersion {
		t.Errorf("schema_version = %d, want %d", tel.SchemaVersion, driver.SchemaVersion)
	}
	if tel.Driver != driver.Bambu {
		t.Errorf("driver = %q, want bambu", tel.Driver)
	}
	if tel.State != driver.StatePrinting {
		t.Errorf("state = %q, want printing", tel.State)
	}
	if tel.Job == nil {
		t.Fatal("expected a job")
	}
	if tel.Job.Filename != "benchy" {
		t.Errorf("filename = %q, want benchy (subtask_name preferred)", tel.Job.Filename)
	}
	if tel.Job.Progress != 0.42 {
		t.Errorf("progress = %v, want 0.42 (fraction)", tel.Job.Progress)
	}
	if tel.Job.RemainingS == nil || *tel.Job.RemainingS != 1740 {
		t.Errorf("remaining_s = %v, want 1740 (29 min × 60)", tel.Job.RemainingS)
	}
	if tel.Job.CurrentLayer == nil || *tel.Job.CurrentLayer != 84 {
		t.Errorf("current_layer = %v, want 84", tel.Job.CurrentLayer)
	}
	if tel.Job.TotalLayers == nil || *tel.Job.TotalLayers != 200 {
		t.Errorf("total_layers = %v, want 200", tel.Job.TotalLayers)
	}
	if tel.Temps == nil || tel.Temps.Nozzle == nil || tel.Temps.Bed == nil || tel.Temps.Chamber == nil {
		t.Fatal("expected nozzle, bed and chamber temps")
	}
	if tel.Temps.Nozzle.Actual != 219.4 || tel.Temps.Nozzle.Target != 220.0 {
		t.Errorf("nozzle = %+v, want {219.4, 220}", tel.Temps.Nozzle)
	}
	if tel.Temps.Bed.Actual != 60.1 || tel.Temps.Bed.Target != 60.0 {
		t.Errorf("bed = %+v, want {60.1, 60}", tel.Temps.Bed)
	}
}

func TestNormalizeStateMapping(t *testing.T) {
	cases := map[string]string{
		"RUNNING":  driver.StatePrinting,
		"PAUSE":    driver.StatePaused,
		"FINISH":   driver.StateComplete,
		"FAILED":   driver.StateError,
		"PREPARE":  driver.StatePrinting,
		"SLICING":  driver.StatePrinting,
		"IDLE":     driver.StateIdle,
		"":         driver.StateIdle,
		"WHATEVER": driver.StateIdle,
	}
	for gcodeState, want := range cases {
		report := map[string]any{"print": map[string]any{"gcode_state": gcodeState}}
		if got := Normalize(report).State; got != want {
			t.Errorf("gcode_state %q -> %q, want %q", gcodeState, got, want)
		}
	}
}

func TestNormalizeFailedSurfacesError(t *testing.T) {
	report := map[string]any{"print": map[string]any{
		"gcode_state": "FAILED",
		"print_error": 83902464.0,
	}}
	tel := Normalize(report)
	if tel.State != driver.StateError {
		t.Fatalf("state = %q, want error", tel.State)
	}
	if tel.Error == nil || *tel.Error == "" {
		t.Fatal("expected a non-empty error message on a failed print")
	}
}

func TestNormalizeIgnoresPrintErrorWhenNotFailed(t *testing.T) {
	// print_error can be non-zero during a healthy print (HMS notices); state
	// must come from gcode_state, and Error must stay nil.
	report := map[string]any{"print": map[string]any{
		"gcode_state": "RUNNING",
		"print_error": 12345.0,
	}}
	tel := Normalize(report)
	if tel.State != driver.StatePrinting {
		t.Errorf("state = %q, want printing", tel.State)
	}
	if tel.Error != nil {
		t.Errorf("error = %v, want nil while running", *tel.Error)
	}
}

func TestNormalizeEmptyReport(t *testing.T) {
	tel := Normalize(map[string]any{})
	if tel.State != driver.StateIdle {
		t.Errorf("state = %q, want idle (offline is decided by the connection layer)", tel.State)
	}
	if tel.Job != nil {
		t.Errorf("expected no job, got %+v", tel.Job)
	}
	if tel.Temps != nil {
		t.Errorf("expected no temps, got %+v", tel.Temps)
	}
}

func TestNormalizeProgressClamped(t *testing.T) {
	report := map[string]any{"print": map[string]any{"gcode_state": "RUNNING", "mc_percent": 130.0}}
	if got := Normalize(report).Job.Progress; got != 1.0 {
		t.Errorf("progress = %v, want clamped to 1.0", got)
	}
}
