package cloud

type RegisterRequest struct {
	PairingToken string       `json:"pairing_token"`
	SiteName     string       `json:"site_name,omitempty"`
	Device       DeviceInfo   `json:"device"`
	Printers     []PrinterInfo `json:"printers,omitempty"`
}

type PrinterInfo struct {
	Name   string `json:"name"`
	UIPort int    `json:"ui_port,omitempty"`
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
	Polling struct {
		CommandsSeconds  int `json:"commands_seconds"`
		SnapshotsSeconds int `json:"snapshots_seconds"`
	} `json:"polling"`
}

type RegisteredPrinter struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
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
