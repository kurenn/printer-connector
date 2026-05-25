package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A non-root run must default StateDir to a writable location next to the
// config file, instead of /var/lib/printer-connector (which a non-root process
// cannot create — the "permission denied" failure this fixes).
func TestLoadDefaultsStateDirNextToConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("assumes a non-root test runner")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"cloud_url":"https://example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := filepath.Join(dir, "state")
	if c.StateDir != want {
		t.Errorf("StateDir = %q, want %q", c.StateDir, want)
	}
}

// An explicit state_dir in the config is always honored.
func TestLoadPreservesExplicitStateDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"cloud_url":"https://example.com","state_dir":"/custom/state"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if c.StateDir != "/custom/state" {
		t.Errorf("StateDir = %q, want /custom/state", c.StateDir)
	}
}
