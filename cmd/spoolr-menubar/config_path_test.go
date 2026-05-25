package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigPath(t *testing.T) {
	p := defaultConfigPath()
	if !filepath.IsAbs(p) {
		t.Errorf("expected an absolute path, got %q", p)
	}
	if filepath.Base(p) != "connector.json" {
		t.Errorf("expected basename connector.json, got %q", p)
	}
	if !strings.Contains(p, "Spoolr") {
		t.Errorf("expected the Spoolr app dir in the path, got %q", p)
	}
}
