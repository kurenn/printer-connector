package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultCloudURL is the production cloud URL used when no override is provided
const DefaultCloudURL = "https://www.spoolr.io"

// Printer protocol types. These mirror the driver package's identifiers and the
// print-contracts printer_telemetry.json `driver` enum (kept in sync by a test).
const (
	TypeMoonraker = "moonraker"
	TypeBambu     = "bambu"
	TypePrusaLink = "prusalink"
	TypeSDCP      = "sdcp"
)

// Printer is one printer the connector manages. Type selects the driver; the
// remaining fields are protocol-specific (only the relevant ones are set).
type Printer struct {
	PrinterID int    `json:"printer_id"`
	Type      string `json:"type,omitempty"` // defaults to "moonraker"
	Name      string `json:"name,omitempty"`

	// Moonraker (and other HTTP-based protocols): base URL + UI port.
	BaseURL string `json:"base_url,omitempty"`
	UIPort  int    `json:"ui_port,omitempty"`

	// Bambu Lab: LAN host plus the printer's access code and serial number,
	// required to authenticate the local MQTT/FTPS sessions.
	Host       string `json:"host,omitempty"`
	AccessCode string `json:"access_code,omitempty"`
	Serial     string `json:"serial,omitempty"`
}

type Config struct {
	CloudURL string `json:"cloud_url"`

	PairingToken    string `json:"pairing_token,omitempty"`
	ConnectorID     string `json:"connector_id,omitempty"`
	ConnectorSecret string `json:"connector_secret,omitempty"`

	SiteName string `json:"site_name,omitempty"`

	PollCommandsSeconds  int `json:"poll_commands_seconds,omitempty"`
	PushSnapshotsSeconds int `json:"push_snapshots_seconds,omitempty"`
	HeartbeatSeconds     int `json:"heartbeat_seconds,omitempty"`

	StateDir string `json:"state_dir,omitempty"`

	// Printers is the canonical printer list. Moonraker is a legacy alias kept
	// so existing configs (which used the "moonraker" key) keep working; Load
	// migrates it into Printers, after which Printers is the source of truth.
	Printers  []Printer `json:"printers,omitempty"`
	Moonraker []Printer `json:"moonraker,omitempty"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}

	// Override cloud_url with environment variable if set
	if envURL := os.Getenv("CLOUD_URL"); envURL != "" {
		c.CloudURL = envURL
	}

	// Use default production URL if still empty
	if c.CloudURL == "" {
		c.CloudURL = DefaultCloudURL
	}

	if c.PollCommandsSeconds <= 0 {
		c.PollCommandsSeconds = 3
	}
	if c.PushSnapshotsSeconds <= 0 {
		c.PushSnapshotsSeconds = 30
	}
	if c.HeartbeatSeconds <= 0 {
		c.HeartbeatSeconds = 10
	}
	if c.StateDir == "" {
		c.StateDir = "/var/lib/printer-connector"
	}

	c.migrateLegacy()
	for i := range c.Printers {
		if c.Printers[i].Type == "" {
			c.Printers[i].Type = TypeMoonraker
		}
		// Default ui_port for HTTP printers (vanilla Klipper usually uses 80).
		if c.Printers[i].Type == TypeMoonraker && c.Printers[i].UIPort == 0 {
			c.Printers[i].UIPort = 80
		}
	}

	return &c, nil
}

// migrateLegacy folds the deprecated "moonraker" list into Printers. Idempotent:
// it only acts when Printers is empty and the legacy list is present.
func (c *Config) migrateLegacy() {
	if len(c.Printers) == 0 && len(c.Moonraker) > 0 {
		c.Printers = c.Moonraker
	}
	c.Moonraker = nil
}

func (c *Config) Validate() error {
	if c.CloudURL == "" {
		return errors.New("cloud_url is required")
	}
	if !strings.HasPrefix(c.CloudURL, "http://") && !strings.HasPrefix(c.CloudURL, "https://") {
		return errors.New("cloud_url must start with http:// or https://")
	}

	hasPair := c.PairingToken != ""
	hasCreds := c.ConnectorID != "" && c.ConnectorSecret != ""
	if !hasPair && !hasCreds {
		return errors.New("config must include either pairing_token OR connector_id + connector_secret")
	}
	if hasPair && hasCreds {
		return errors.New("config should not include pairing_token once connector_id + connector_secret exist")
	}

	c.migrateLegacy()
	if len(c.Printers) == 0 {
		return errors.New("printers must include at least one entry")
	}
	seen := map[int]bool{}
	for _, p := range c.Printers {
		// Allow printer_id=0 during initial pairing (will be populated by Rails)
		if p.PrinterID < 0 {
			return errors.New("printer_id must be >= 0")
		}
		// After pairing, printer_id must be set
		if !hasPair && p.PrinterID == 0 {
			return errors.New("printer_id must be > 0 after pairing")
		}
		if p.PrinterID > 0 && seen[p.PrinterID] {
			return fmt.Errorf("duplicate printer_id: %d", p.PrinterID)
		}
		if p.PrinterID > 0 {
			seen[p.PrinterID] = true
		}
		if err := validatePrinter(p); err != nil {
			return err
		}
	}
	return nil
}

// validatePrinter checks the protocol-specific fields for a single printer. Only
// protocols with an implemented driver are accepted; others are rejected until
// their driver lands, so a config can't promise a printer the connector can't drive.
func validatePrinter(p Printer) error {
	t := p.Type
	if t == "" {
		t = TypeMoonraker
	}
	switch t {
	case TypeMoonraker:
		if p.BaseURL == "" {
			return fmt.Errorf("base_url required for moonraker printer_id %d", p.PrinterID)
		}
		if !strings.HasPrefix(p.BaseURL, "http://") && !strings.HasPrefix(p.BaseURL, "https://") {
			return fmt.Errorf("base_url must start with http:// or https:// for printer_id %d", p.PrinterID)
		}
		if strings.Contains(p.BaseURL, "..") {
			return fmt.Errorf("base_url must not contain '..' for printer_id %d", p.PrinterID)
		}
	case TypeBambu:
		if p.Host == "" {
			return fmt.Errorf("host required for bambu printer_id %d", p.PrinterID)
		}
		if p.Serial == "" {
			return fmt.Errorf("serial required for bambu printer_id %d", p.PrinterID)
		}
		if p.AccessCode == "" {
			return fmt.Errorf("access_code required for bambu printer_id %d", p.PrinterID)
		}
	default:
		return fmt.Errorf("unsupported printer type %q for printer_id %d (no driver yet)", p.Type, p.PrinterID)
	}
	return nil
}

// SaveAtomic writes config JSON to disk atomically: write temp + rename.
// Uses 0600 permissions because config stores connector_secret.
func SaveAtomic(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
