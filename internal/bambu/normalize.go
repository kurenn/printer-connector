package bambu

import (
	"strings"

	"printer-connector/internal/driver"
)

// Normalize converts a merged Bambu Lab MQTT report into the canonical
// telemetry shape. Bambu publishes its state under the top-level "print" key as
// a stream of partial updates that the transport deep-merges into one map; this
// function reads that merged map.
//
// It is defensive in the same spirit as the Moonraker normalizer: missing or
// oddly-typed fields yield zero values rather than errors, so a partial report
// still produces usable telemetry. The connection layer — not this function —
// decides offline, so an empty report normalizes to idle, never offline.
func Normalize(report map[string]any) driver.Telemetry {
	t := driver.Telemetry{
		SchemaVersion: driver.SchemaVersion,
		Driver:        driver.Bambu,
		State:         driver.StateIdle,
	}

	p := childMap(report, "print")
	if p == nil {
		return t
	}

	t.State = mapState(getString(p, "gcode_state"))

	job := driver.Job{
		Filename: firstNonEmpty(getString(p, "subtask_name"), getString(p, "gcode_file")),
		Progress: clampFraction(getFloat(p, "mc_percent") / 100.0),
	}
	// Bambu reports remaining time in whole minutes; the contract wants seconds.
	if remaining, ok := numPtr(p, "mc_remaining_time"); ok && *remaining >= 0 {
		secs := int64(*remaining * 60)
		job.RemainingS = &secs
	}
	if layer, ok := intPtr(p, "layer_num"); ok {
		job.CurrentLayer = layer
	}
	if total, ok := intPtr(p, "total_layer_num"); ok {
		job.TotalLayers = total
	}
	t.Job = &job

	temps := driver.Temps{}
	if hasAny(p, "nozzle_temper", "nozzle_target_temper") {
		temps.Nozzle = &driver.Sensor{
			Actual: getFloat(p, "nozzle_temper"),
			Target: getFloat(p, "nozzle_target_temper"),
		}
	}
	if hasAny(p, "bed_temper", "bed_target_temper") {
		temps.Bed = &driver.Sensor{
			Actual: getFloat(p, "bed_temper"),
			Target: getFloat(p, "bed_target_temper"),
		}
	}
	if hasAny(p, "chamber_temper", "chamber_target_temper") && hasChamberSensor(printerModel(report)) {
		temps.Chamber = &driver.Sensor{
			Actual: getFloat(p, "chamber_temper"),
			Target: getFloat(p, "chamber_target_temper"),
		}
	}
	if temps.Nozzle != nil || temps.Bed != nil || temps.Chamber != nil {
		t.Temps = &temps
	}

	// Surface a print error only on a real failure. print_error is a status
	// word that can be non-zero (HMS notices) during an otherwise healthy
	// print, so gcode_state is the source of truth for the error *state*.
	if t.State == driver.StateError {
		if code := getFloat(p, "print_error"); code != 0 {
			msg := bambuErrorMessage(int64(code))
			t.Error = &msg
		}
	}

	return t
}

// openFrameModels are Bambu printers with no enclosure, and therefore no
// chamber thermistor. They still publish chamber_temper, but as a fixed
// placeholder rather than a reading: an A1 mini reports exactly 5 whether it is
// mid-print with the bed at 65°C or cooling down afterwards. Forwarding that
// would show a temperature the printer never measured, so these models report
// no chamber sensor at all — the field is optional in the contract.
//
// Matching is on whole tokens of the product name ("Bambu Lab A1 mini"), not
// substrings, so "P1P" never matches a "P1S" and vice versa.
var openFrameModels = map[string]bool{"A1": true, "P1P": true}

// hasChamberSensor reports whether a model measures chamber temperature. An
// unrecognised model is trusted: dropping a real reading is worse than passing
// through one more placeholder, and unknown models get reported as bugs.
func hasChamberSensor(productName string) bool {
	if productName == "" {
		return true
	}
	for _, tok := range strings.Fields(strings.ToUpper(productName)) {
		if openFrameModels[tok] {
			return false
		}
	}
	return true
}

// printerModel pulls the product name out of the get_version reply that the
// transport merges into the report under "info". Returns "" before the reply
// lands (or if the printer never sends one).
func printerModel(report map[string]any) string {
	info := childMap(report, "info")
	if info == nil {
		return ""
	}
	modules, ok := info["module"].([]any)
	if !ok {
		return ""
	}
	for _, m := range modules {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if name := getString(mm, "product_name"); name != "" {
			return name
		}
	}
	return ""
}

// mapState maps Bambu's gcode_state to a canonical state. PREPARE/SLICING are
// active phases of a job, so they map to printing rather than idle.
func mapState(s string) string {
	switch s {
	case "RUNNING":
		return driver.StatePrinting
	case "PAUSE":
		return driver.StatePaused
	case "FINISH":
		return driver.StateComplete
	case "FAILED":
		return driver.StateError
	case "PREPARE", "SLICING":
		return driver.StatePrinting
	default: // "IDLE", "", and anything unexpected
		return driver.StateIdle
	}
}

func bambuErrorMessage(code int64) string {
	// We don't ship the full HMS code table; the numeric code is enough for the
	// cloud to display and look up. Kept as a helper so a table can be added
	// without touching Normalize.
	return "print_error " + itoa(code)
}
