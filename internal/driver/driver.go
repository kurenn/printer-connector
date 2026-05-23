// Package driver defines the protocol-agnostic contract that every printer
// family (Moonraker, Bambu, PrusaLink, Elegoo SDCP) implements, together with
// the canonical telemetry shape shared with the cloud via print-contracts
// (schemas/printer_telemetry.json, schema_version 1).
//
// The agent talks to printers exclusively through Driver, so adding a protocol
// means adding an implementation — not branching on printer type throughout the
// agent or the cloud.
package driver

import "context"

// Driver is the seam between the agent and a printer's native protocol.
//
// QueryObjects exposes the raw protocol response and is retained for the
// snapshot "raw" payload and reachability checks during the migration window
// (snapshots carry both raw and normalized telemetry). Telemetry is the
// canonical, protocol-agnostic view the cloud consumes.
type Driver interface {
	// Capabilities reports which command actions this printer supports, so the
	// cloud offers only supported controls and the agent can reject the rest.
	Capabilities() []string

	// Telemetry returns the normalized printer state (canonical schema).
	Telemetry(ctx context.Context) (Telemetry, error)

	// QueryObjects returns the raw protocol state.
	QueryObjects(ctx context.Context) (map[string]any, error)

	// NormalizeRaw converts this driver's raw response (as returned by
	// QueryObjects) into canonical telemetry without performing I/O. The agent
	// uses it to attach payload["normalized"] alongside the raw snapshot it just
	// fetched, avoiding a second query.
	NormalizeRaw(raw map[string]any) Telemetry

	// Print control.
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Cancel(ctx context.Context) error
	Home(ctx context.Context, axes ...string) error
	StartPrint(ctx context.Context, filename string) error

	// File and history operations.
	UploadFile(ctx context.Context, filename string, content []byte) error
	DeleteFile(ctx context.Context, filename string) error
	ListFiles(ctx context.Context) ([]map[string]any, error)
	GetHistory(ctx context.Context, limit int) (map[string]any, error)

	// Webcam.
	GetWebcamSnapshot(ctx context.Context) ([]byte, string, error)
}

// SchemaVersion is the current canonical telemetry schema version.
const SchemaVersion = 1

// Driver identifiers — the printer_telemetry.json `driver` enum.
const (
	Moonraker = "moonraker"
	Bambu     = "bambu"
	PrusaLink = "prusalink"
	SDCP      = "sdcp"
)

// Canonical print states — the printer_telemetry.json `state` enum.
const (
	StateIdle     = "idle"
	StatePrinting = "printing"
	StatePaused   = "paused"
	StateError    = "error"
	StateOffline  = "offline"
	StateComplete = "complete"
)

// Telemetry is the normalized printer state. It mirrors
// print-contracts/schemas/printer_telemetry.json; drivers are responsible for
// translating their native protocol into this shape.
type Telemetry struct {
	SchemaVersion int     `json:"schema_version"`
	Driver        string  `json:"driver"`
	Model         string  `json:"model,omitempty"`
	Firmware      string  `json:"firmware,omitempty"`
	State         string  `json:"state"`
	Error         *string `json:"error,omitempty"`
	Job           *Job    `json:"job,omitempty"`
	Temps         *Temps  `json:"temps,omitempty"`
}

// Job describes the active or most recent print.
type Job struct {
	Filename     string  `json:"filename,omitempty"`
	Progress     float64 `json:"progress"`
	ElapsedS     int64   `json:"elapsed_s"`
	RemainingS   *int64  `json:"remaining_s,omitempty"`
	CurrentLayer *int    `json:"current_layer,omitempty"`
	TotalLayers  *int    `json:"total_layers,omitempty"`
}

// Temps groups the thermal sensors a printer exposes. A nil pointer means the
// printer does not report that sensor.
type Temps struct {
	Nozzle  *Sensor `json:"nozzle,omitempty"`
	Bed     *Sensor `json:"bed,omitempty"`
	Chamber *Sensor `json:"chamber,omitempty"`
}

// Sensor is an actual/target temperature pair in degrees Celsius.
type Sensor struct {
	Actual float64 `json:"actual"`
	Target float64 `json:"target"`
}
