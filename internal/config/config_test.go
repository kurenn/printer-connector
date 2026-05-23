package config

import (
	"os"
	"path/filepath"
	"testing"
)

// validConfig returns a paired config that passes Validate, which tests mutate.
func validConfig() *Config {
	return &Config{
		CloudURL:        "https://www.spoolr.io",
		ConnectorID:     "42",
		ConnectorSecret: "s3cret",
		Moonraker: []MoonrakerPrinter{
			{PrinterID: 1, BaseURL: "http://localhost:7125"},
		},
	}
}

func TestValidate_Valid(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
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
		"no printers":         func(c *Config) { c.Moonraker = nil },
		"duplicate printer_id": func(c *Config) {
			c.Moonraker = []MoonrakerPrinter{
				{PrinterID: 1, BaseURL: "http://a:7125"},
				{PrinterID: 1, BaseURL: "http://b:7125"},
			}
		},
		"missing base_url":  func(c *Config) { c.Moonraker[0].BaseURL = "" },
		"non-http base_url": func(c *Config) { c.Moonraker[0].BaseURL = "tcp://x" },
		"base_url with ..":  func(c *Config) { c.Moonraker[0].BaseURL = "http://x/../y" },
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
		Moonraker:    []MoonrakerPrinter{{PrinterID: 0, BaseURL: "http://localhost:7125"}},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected pairing config with printer_id 0 to be valid, got %v", err)
	}
}

func TestLoad_AppliesDefaults(t *testing.T) {
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
	if cfg.Moonraker[0].UIPort != 80 {
		t.Errorf("UIPort default = %d, want 80", cfg.Moonraker[0].UIPort)
	}
}
