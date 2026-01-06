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

type MoonrakerPrinter struct {
	PrinterID int    `json:"printer_id"`
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	UIPort    int    `json:"ui_port,omitempty"`
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

	StateDir  string             `json:"state_dir,omitempty"`
	Moonraker []MoonrakerPrinter `json:"moonraker"`
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

	return &c, nil
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

	if len(c.Moonraker) == 0 {
		return errors.New("moonraker must include at least one printer entry")
	}
	seen := map[int]bool{}
	for _, p := range c.Moonraker {
		// Allow printer_id=0 during initial pairing (will be populated by Rails)
		if p.PrinterID < 0 {
			return fmt.Errorf("moonraker printer_id must be >= 0")
		}
		// After pairing, printer_id must be set
		if !hasPair && p.PrinterID == 0 {
			return fmt.Errorf("moonraker printer_id must be > 0 after pairing")
		}
		if p.PrinterID > 0 && seen[p.PrinterID] {
			return fmt.Errorf("duplicate moonraker printer_id: %d", p.PrinterID)
		}
		if p.PrinterID > 0 {
			seen[p.PrinterID] = true
		}
		if p.BaseURL == "" {
			return fmt.Errorf("moonraker base_url required for printer_id %d", p.PrinterID)
		}
		if !strings.HasPrefix(p.BaseURL, "http://") && !strings.HasPrefix(p.BaseURL, "https://") {
			return fmt.Errorf("moonraker base_url must start with http:// or https:// for printer_id %d", p.PrinterID)
		}
		if strings.Contains(p.BaseURL, "..") {
			return fmt.Errorf("moonraker base_url must not contain '..' for printer_id %d", p.PrinterID)
		}
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
