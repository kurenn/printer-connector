package moonraker

import (
	"context"

	"printer-connector/internal/driver"
)

// Client is the first driver.Driver implementation.
var _ driver.Driver = (*Client)(nil)

// Capabilities reports the command actions a Klipper/Moonraker printer supports.
func (c *Client) Capabilities() []string {
	return []string{
		"pause", "resume", "cancel", "start_print", "upload_file",
		"delete_file", "sync_files", "import_history", "create_backup",
		"homing", "webcam",
	}
}

// Telemetry queries the printer and normalizes Moonraker's object model into the
// canonical telemetry shape.
func (c *Client) Telemetry(ctx context.Context) (driver.Telemetry, error) {
	raw, err := c.QueryObjects(ctx)
	if err != nil {
		return driver.Telemetry{}, err
	}
	return Normalize(raw), nil
}

// NormalizeRaw converts an already-fetched QueryObjects response into canonical
// telemetry without performing I/O.
func (c *Client) NormalizeRaw(raw map[string]any) driver.Telemetry {
	return Normalize(raw)
}

// Normalize converts a Moonraker /printer/objects/query response into canonical
// telemetry. It is defensive: missing or oddly-typed fields yield zero values
// rather than errors, so a partial Moonraker response still produces telemetry.
func Normalize(raw map[string]any) driver.Telemetry {
	status := nestedMap(raw, "result", "status")

	t := driver.Telemetry{
		SchemaVersion: driver.SchemaVersion,
		Driver:        driver.Moonraker,
		State:         driver.StateIdle,
	}

	printStats := childMap(status, "print_stats")
	t.State = moonrakerState(
		getString(printStats, "state"),
		getBool(childMap(status, "virtual_sdcard"), "is_active"),
		getBool(childMap(status, "pause_resume"), "is_paused"),
	)

	job := driver.Job{
		Filename: getString(printStats, "filename"),
		ElapsedS: int64(getFloat(printStats, "print_duration")),
	}
	if vs := childMap(status, "virtual_sdcard"); vs != nil {
		job.Progress = getFloat(vs, "progress")
	}
	if info := childMap(printStats, "info"); info != nil {
		if cl, ok := intPtr(info, "current_layer"); ok {
			job.CurrentLayer = cl
		}
		if tl, ok := intPtr(info, "total_layer"); ok {
			job.TotalLayers = tl
		}
	}
	t.Job = &job

	temps := driver.Temps{}
	if e := childMap(status, "extruder"); e != nil {
		temps.Nozzle = &driver.Sensor{Actual: getFloat(e, "temperature"), Target: getFloat(e, "target")}
	}
	if b := childMap(status, "heater_bed"); b != nil {
		temps.Bed = &driver.Sensor{Actual: getFloat(b, "temperature"), Target: getFloat(b, "target")}
	}
	if temps.Nozzle != nil || temps.Bed != nil {
		t.Temps = &temps
	}

	return t
}

// moonrakerState derives the canonical state from Klipper's print_stats.state
// plus the virtual_sdcard/pause_resume flags. The precedence mirrors the cloud's
// raw Printers::Status so emitting normalized telemetry doesn't change a live
// printer's reported state: error > paused (is_paused or state) > printing
// (is_active or state) > complete > idle. "cancelled"/"standby"/"ready"/unknown
// all fall through to idle.
func moonrakerState(state string, isActive, isPaused bool) string {
	switch {
	case state == "error":
		return driver.StateError
	case isPaused || state == "paused":
		return driver.StatePaused
	case isActive || state == "printing":
		return driver.StatePrinting
	case state == "complete":
		return driver.StateComplete
	default:
		return driver.StateIdle
	}
}

func getBool(m map[string]any, k string) bool {
	if m == nil {
		return false
	}
	v, _ := m[k].(bool)
	return v
}

func nestedMap(m map[string]any, keys ...string) map[string]any {
	cur := m
	for _, k := range keys {
		cur = childMap(cur, k)
	}
	return cur
}

func childMap(m map[string]any, k string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[k].(map[string]any); ok {
		return v
	}
	return nil
}

func getString(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func getFloat(m map[string]any, k string) float64 {
	if m == nil {
		return 0
	}
	switch v := m[k].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

func intPtr(m map[string]any, k string) (*int, bool) {
	if m == nil {
		return nil, false
	}
	switch v := m[k].(type) {
	case float64:
		i := int(v)
		return &i, true
	case int:
		i := v
		return &i, true
	}
	return nil, false
}
