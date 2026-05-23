package config

import (
	"os"
	"path/filepath"
	"testing"

	"printer-connector/internal/driver"
)

// validConfig returns a paired config that passes Validate, which tests mutate.
func validConfig() *Config {
	return &Config{
		CloudURL:        "https://www.spoolr.io",
		ConnectorID:     "42",
		ConnectorSecret: "s3cret",
		Printers: []Printer{
			{PrinterID: 1, Type: TypeMoonraker, BaseURL: "http://localhost:7125"},
		},
	}
}

func bambuConfig() *Config {
	return &Config{
		CloudURL:        "https://www.spoolr.io",
		ConnectorID:     "42",
		ConnectorSecret: "s3cret",
		Printers: []Printer{
			{PrinterID: 1, Type: TypeBambu, Host: "192.168.1.50", Serial: "01ABC", AccessCode: "12345678"},
		},
	}
}

func TestValidate_Valid(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("expected valid moonraker config, got %v", err)
	}
	if err := bambuConfig().Validate(); err != nil {
		t.Fatalf("expected valid bambu config, got %v", err)
	}
}

func TestValidate_Rejections(t *testing.T) {
	cases := map[string]func(*Config){
		"missing cloud_url":  func(c *Config) { c.CloudURL = "" },
		"non-http cloud_url": func(c *Config) { c.CloudURL = "ftp://x" },
		"neither pair nor creds": func(c *Config) {
			c.ConnectorID, c.ConnectorSecret, c.PairingToken = "", "", ""
		},
		"both pair and creds": func(c *Config) { c.PairingToken = "tok" },
		"no printers":         func(c *Config) { c.Printers = nil },
		"duplicate printer_id": func(c *Config) {
			c.Printers = []Printer{
				{PrinterID: 1, BaseURL: "http://a:7125"},
				{PrinterID: 1, BaseURL: "http://b:7125"},
			}
		},
		"missing base_url":   func(c *Config) { c.Printers[0].BaseURL = "" },
		"non-http base_url":  func(c *Config) { c.Printers[0].BaseURL = "tcp://x" },
		"base_url with ..":   func(c *Config) { c.Printers[0].BaseURL = "http://x/../y" },
		"unsupported type":   func(c *Config) { c.Printers[0].Type = "octoprint" },
		"bambu missing host": func(c *Config) { *c = *bambuConfig(); c.Printers[0].Host = "" },
		"bambu missing serial": func(c *Config) {
			*c = *bambuConfig()
			c.Printers[0].Serial = ""
		},
		"bambu missing access_code": func(c *Config) {
			*c = *bambuConfig()
			c.Printers[0].AccessCode = ""
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := validConfig()
			mutate(c)
			if err := c.Validate(); err == nil {
				t.Fatalf("expected error for %q, got nil", name)
			}
		})
	}
}

// During pairing, printer_id 0 is allowed because Rails assigns it on register.
func TestValidate_PairingAllowsZeroPrinterID(t *testing.T) {
	c := &Config{
		CloudURL:     "https://www.spoolr.io",
		PairingToken: "tok",
		Printers:     []Printer{{Type: TypeMoonraker, BaseURL: "http://localhost:7125"}},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected pairing config with printer_id 0 to be valid, got %v", err)
	}
}

// Legacy configs used the "moonraker" key with no type; Load must migrate them
// into Printers, default the type to moonraker, and apply the ui_port default.
func TestLoad_MigratesLegacyMoonrakerAndDefaults(t *testing.T) {
	t.Setenv("CLOUD_URL", "") // ignore any ambient override
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
	  "connector_id": "1",
	  "connector_secret": "x",
	  "moonraker": [{"printer_id": 1, "base_url": "http://localhost:7125"}]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CloudURL != DefaultCloudURL {
		t.Errorf("CloudURL = %q, want default %q", cfg.CloudURL, DefaultCloudURL)
	}
	if cfg.PollCommandsSeconds != 3 || cfg.PushSnapshotsSeconds != 30 || cfg.HeartbeatSeconds != 10 {
		t.Errorf("poll defaults not applied: %+v", cfg)
	}
	if cfg.StateDir == "" {
		t.Error("StateDir default not applied")
	}
	if len(cfg.Moonraker) != 0 {
		t.Errorf("legacy Moonraker field should be cleared after migration, got %+v", cfg.Moonraker)
	}
	if len(cfg.Printers) != 1 {
		t.Fatalf("expected 1 migrated printer, got %d", len(cfg.Printers))
	}
	if cfg.Printers[0].Type != TypeMoonraker {
		t.Errorf("migrated type = %q, want moonraker", cfg.Printers[0].Type)
	}
	if cfg.Printers[0].UIPort != 80 {
		t.Errorf("UIPort default = %d, want 80", cfg.Printers[0].UIPort)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("migrated config should validate, got %v", err)
	}
}

func TestLoad_BambuPrinter(t *testing.T) {
	t.Setenv("CLOUD_URL", "")
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
	  "connector_id": "1",
	  "connector_secret": "x",
	  "printers": [{"printer_id": 5, "type": "bambu", "host": "192.168.1.50", "serial": "01ABC", "access_code": "12345678"}]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Printers[0].Type != TypeBambu || cfg.Printers[0].Serial != "01ABC" {
		t.Errorf("bambu printer not parsed: %+v", cfg.Printers[0])
	}
	// Bambu printers must NOT get the moonraker ui_port default.
	if cfg.Printers[0].UIPort != 0 {
		t.Errorf("bambu UIPort = %d, want 0 (no default)", cfg.Printers[0].UIPort)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("bambu config should validate, got %v", err)
	}
}

// The config type constants must stay in lockstep with the driver identifiers so
// dispatch and the canonical telemetry `driver` field never drift.
func TestTypeConstantsMatchDriver(t *testing.T) {
	pairs := [][2]string{
		{TypeMoonraker, driver.Moonraker},
		{TypeBambu, driver.Bambu},
		{TypePrusaLink, driver.PrusaLink},
		{TypeSDCP, driver.SDCP},
	}
	for _, p := range pairs {
		if p[0] != p[1] {
			t.Errorf("config type %q != driver id %q", p[0], p[1])
		}
	}
}
