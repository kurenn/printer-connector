package bambu

import (
	"bytes"
	"encoding/json"
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

// Open-frame models publish chamber_temper as a placeholder, not a reading.
// Verified on an A1 mini: it reported exactly 5 both mid-print (bed 65°C) and
// while cooling down afterwards, never varying.
func TestChamberOmittedForOpenFrameModels(t *testing.T) {
	reportFor := func(product string) map[string]any {
		return map[string]any{
			"print": map[string]any{
				"gcode_state":    "RUNNING",
				"chamber_temper": 5.0,
				"nozzle_temper":  220.0,
			},
			"info": map[string]any{
				"module": []any{
					map[string]any{"name": "ota", "product_name": product},
				},
			},
		}
	}

	tests := []struct {
		product     string
		wantChamber bool
	}{
		{"Bambu Lab A1 mini", false},
		{"Bambu Lab A1", false},
		{"Bambu Lab P1P", false},
		{"Bambu Lab P1S", true},
		{"Bambu Lab X1 Carbon", true},
		{"Bambu Lab X1E", true},
		{"", true}, // model unknown (reply not yet received) — trust the value
	}

	for _, tt := range tests {
		name := tt.product
		if name == "" {
			name = "unknown model"
		}
		t.Run(name, func(t *testing.T) {
			got := Normalize(reportFor(tt.product))
			if got.Temps == nil {
				t.Fatal("Temps = nil, want temps present")
			}
			if hasChamber := got.Temps.Chamber != nil; hasChamber != tt.wantChamber {
				t.Errorf("chamber present = %v, want %v", hasChamber, tt.wantChamber)
			}
			// The nozzle must survive regardless — gating is chamber-only.
			if got.Temps.Nozzle == nil {
				t.Error("nozzle sensor was dropped")
			}
		})
	}
}

func TestPrinterModel(t *testing.T) {
	tests := []struct {
		name   string
		report map[string]any
		want   string
	}{
		{"no info key", map[string]any{"print": map[string]any{}}, ""},
		{"info without modules", map[string]any{"info": map[string]any{}}, ""},
		{
			// Real A1 mini shape: the first modules carry an empty product_name.
			name: "skips modules without a product name",
			report: map[string]any{"info": map[string]any{"module": []any{
				map[string]any{"name": "esp32", "product_name": ""},
				map[string]any{"name": "ota", "product_name": "Bambu Lab A1 mini"},
			}}},
			want: "Bambu Lab A1 mini",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := printerModel(tt.report); got != tt.want {
				t.Errorf("printerModel = %q, want %q", got, tt.want)
			}
		})
	}
}

// Bambu's LAN protocol has no elapsed field, so elapsed must be absent rather
// than a hard 0 — a zero made the cloud's elapsed+remaining total under-report.
// Real A1 mini shape: mc_percent/layers/remaining present, nothing for elapsed.
func TestElapsedIsAbsentForBambu(t *testing.T) {
	report := map[string]any{"print": map[string]any{
		"gcode_state":       "RUNNING",
		"mc_percent":        62.0,
		"mc_remaining_time": 18.0,
		"layer_num":         113.0,
		"total_layer_num":   240.0,
	}}

	got := Normalize(report)
	if got.Job == nil {
		t.Fatal("Job = nil")
	}
	if got.Job.ElapsedS != nil {
		t.Errorf("elapsed_s = %v, want nil (Bambu never reports elapsed)", *got.Job.ElapsedS)
	}
	// The fields Bambu does report must still come through.
	if got.Job.RemainingS == nil || *got.Job.RemainingS != 1080 {
		t.Errorf("remaining_s = %v, want 1080", got.Job.RemainingS)
	}

	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte("elapsed_s")) {
		t.Errorf("elapsed_s should not appear on the wire for Bambu, got: %s", b)
	}
}
