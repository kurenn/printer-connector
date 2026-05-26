package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"printer-connector/internal/config"
	"printer-connector/internal/driver"
)

// statusFile is the top-level structure written to status.json each cycle.
type statusFile struct {
	SchemaVersion int             `json:"schema_version"`
	UpdatedAt     string          `json:"updated_at"`
	SiteName      string          `json:"site_name,omitempty"`
	Printers      []printerStatus `json:"printers"`
}

// printerStatus holds the per-printer snapshot visible to local consumers (e.g.
// the macOS menu-bar app) without requiring a cloud round-trip.
type printerStatus struct {
	PrinterID    int      `json:"printer_id"`
	Name         string   `json:"name,omitempty"`
	Kind         string   `json:"kind"`
	Reachable    bool     `json:"reachable"`
	State        string   `json:"state"`
	Progress     *float64 `json:"progress,omitempty"`
	Job          string   `json:"job,omitempty"`
	RemainingS   *int64   `json:"remaining_s,omitempty"`
	LayerCurrent *int     `json:"layer_current,omitempty"`
	LayerTotal   *int     `json:"layer_total,omitempty"`
	NozzleC      *float64 `json:"nozzle_c,omitempty"`
	BedC         *float64 `json:"bed_c,omitempty"`
	Error        *string  `json:"error,omitempty"`
}

// printerKind maps a config type string to the status.json "kind" field.
// Moonraker is the Klipper HTTP API, so we surface it as "klipper". Bambu
// stays "bambu". Anything else falls back to "printer".
func printerKind(t string) string {
	switch t {
	case config.TypeBambu:
		return "bambu"
	case config.TypeMoonraker, "":
		return "klipper"
	default:
		return "printer"
	}
}

// buildPrinterStatus converts a normalized driver.Telemetry (and reachability
// flag) into a printerStatus entry for the status file.
func buildPrinterStatus(p config.Printer, tel driver.Telemetry, reachable bool) printerStatus {
	kind := printerKind(p.Type)

	ps := printerStatus{
		PrinterID: p.PrinterID,
		Name:      p.Name,
		Kind:      kind,
		Reachable: reachable,
		State:     tel.State,
		Error:     tel.Error,
	}

	if !reachable || tel.State == "" {
		ps.State = driver.StateOffline
	}

	if tel.Job != nil {
		if tel.Job.Filename != "" {
			ps.Job = tel.Job.Filename
		}
		prog := tel.Job.Progress
		ps.Progress = &prog
		ps.RemainingS = tel.Job.RemainingS
		ps.LayerCurrent = tel.Job.CurrentLayer
		ps.LayerTotal = tel.Job.TotalLayers
	}

	if tel.Temps != nil {
		if tel.Temps.Nozzle != nil {
			v := tel.Temps.Nozzle.Actual
			ps.NozzleC = &v
		}
		if tel.Temps.Bed != nil {
			v := tel.Temps.Bed.Actual
			ps.BedC = &v
		}
	}

	return ps
}

// writeStatusFile writes a status.json beside the connector config file using
// an atomic temp-file + rename. Errors are best-effort: the caller should log
// them at warn level without aborting the snapshot loop.
func writeStatusFile(cfgPath string, cfg *config.Config, entries []printerStatus) error {
	sf := statusFile{
		SchemaVersion: 1,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		SiteName:      cfg.SiteName,
		Printers:      entries,
	}

	b, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	dir := filepath.Dir(cfgPath)
	dest := filepath.Join(dir, "status.json")
	tmp := dest + ".tmp"

	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
