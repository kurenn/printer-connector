package cloud

type RegisterRequest struct {
	PairingToken string        `json:"pairing_token"`
	SiteName     string        `json:"site_name,omitempty"`
	Device       DeviceInfo    `json:"device"`
	Printers     []PrinterInfo `json:"printers,omitempty"`
}

type PrinterInfo struct {
	Name string `json:"name"`
	// Type is the protocol/driver ("moonraker", "bambu", …) so the cloud creates
	// the printer with the right printer_type instead of defaulting to moonraker.
	Type string `json:"type,omitempty"`
	// Host is the printer's LAN address — Bambu printers carry their own; Moonraker
	// printers report theirs from discovery. Credentials stay on the connector.
	Host          string `json:"host,omitempty"`
	MoonrakerPort int    `json:"moonraker_port,omitempty"`
	UIPort        int    `json:"ui_port,omitempty"`
}

type DeviceInfo struct {
	Hostname string `json:"hostname,omitempty"`
	Arch     string `json:"arch,omitempty"`
	OS       string `json:"os,omitempty"`
	Version  string `json:"version,omitempty"`
	IP       string `json:"ip,omitempty"`
	UIPort   int    `json:"ui_port,omitempty"`
}

type RegisterResponse struct {
	Connector struct {
		ID StringOrNumber `json:"id"`
	} `json:"connector"`
	Credentials struct {
		Secret string `json:"secret"`
	} `json:"credentials"`
	Printers []RegisteredPrinter `json:"printers,omitempty"`
	Polling  struct {
		CommandsSeconds  int `json:"commands_seconds"`
		SnapshotsSeconds int `json:"snapshots_seconds"`
	} `json:"polling"`
}

type RegisteredPrinter struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// RegisterPrintersRequest is the body for POST /api/v1/connectors/:id/printers,
// used by a paired connector to adopt printers it discovered after pairing.
type RegisterPrintersRequest struct {
	Printers []PrinterInfo `json:"printers"`
}

// RegisterPrintersResponse echoes the adopted printers (host+port included so
// the connector can match them back to what it discovered, regardless of order
// or any cross-user entries the cloud skipped).
type RegisterPrintersResponse struct {
	Printers []AdoptedPrinter `json:"printers"`
}

type AdoptedPrinter struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Host          string `json:"host"`
	MoonrakerPort int    `json:"moonraker_port"`
}

type HeartbeatRequest struct {
	Status struct {
		UptimeSeconds int64  `json:"uptime_seconds"`
		Version       string `json:"version,omitempty"`
	} `json:"status"`
	Printers []HeartbeatPrinter `json:"printers,omitempty"`
}

type HeartbeatPrinter struct {
	PrinterID int  `json:"printer_id"`
	Reachable bool `json:"reachable"`
}

type Command struct {
	ID        StringOrNumber `json:"id"`
	PrinterID int            `json:"printer_id"`
	Action    string         `json:"action"`
	Params    map[string]any `json:"params"`
}

type CommandCompleteRequest struct {
	Status       string         `json:"status"`
	Result       map[string]any `json:"result,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
}

type SnapshotsBatchRequest struct {
	Snapshots []Snapshot `json:"snapshots"`
}

type Snapshot struct {
	PrinterID  int            `json:"printer_id"`
	CapturedAt string         `json:"captured_at"`
	Payload    map[string]any `json:"payload"`
}

type SnapshotsBatchResponse struct {
	Inserted int `json:"inserted"`
}

// WebcamRequest represents a pending webcam snapshot request from Rails
type WebcamRequest struct {
	ID        StringOrNumber `json:"id"`
	PrinterID int            `json:"printer_id"`
	CreatedAt string         `json:"created_at,omitempty"`
}

// WebcamStreamRequest is a printer a browser is actively watching, returned by
// the stream-poll endpoint. The connector relays a high-cadence MJPEG feed for
// it until ExpiresInMs elapses (the viewer refreshes the window by continuing
// to watch). This is the "stream mode" path that makes the live feed smooth;
// the per-frame snapshot relay is the always-present fallback.
type WebcamStreamRequest struct {
	PrinterID   int `json:"printer_id"`
	ExpiresInMs int `json:"expires_in_ms"`
}
