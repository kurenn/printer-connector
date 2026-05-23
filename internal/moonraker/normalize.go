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
	t.State = mapState(getString(printStats, "state"))

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

// mapState maps Klipper print_stats.state to a canonical state.
func mapState(s string) string {
	switch s {
	case "printing":
		return driver.StatePrinting
	case "paused":
		return driver.StatePaused
	case "complete":
		return driver.StateComplete
	case "error":
		return driver.StateError
	default: // "standby", "cancelled", "" and anything unexpected
		return driver.StateIdle
	}
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
