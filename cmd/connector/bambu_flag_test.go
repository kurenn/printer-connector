package main

import (
	"strings"
	"testing"
)

func TestBambuFlag_SetAndString(t *testing.T) {
	var f bambuFlag

	if s := f.String(); s != "" {
		t.Errorf("empty bambuFlag.String() = %q, want \"\"", s)
	}

	if err := f.Set("192.168.1.10,01ABC,12345678,My Printer"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := f.Set("192.168.1.11,02DEF,87654321,Other"); err != nil {
		t.Fatalf("Set #2: %v", err)
	}

	if len(f) != 2 {
		t.Fatalf("len = %d, want 2", len(f))
	}
	if f[0] != "192.168.1.10,01ABC,12345678,My Printer" {
		t.Errorf("f[0] = %q, want first spec", f[0])
	}
	if f[1] != "192.168.1.11,02DEF,87654321,Other" {
		t.Errorf("f[1] = %q, want second spec", f[1])
	}

	combined := f.String()
	if !strings.Contains(combined, "192.168.1.10") || !strings.Contains(combined, "192.168.1.11") {
		t.Errorf("String() = %q, should contain both specs", combined)
	}
}

func TestBambuFlag_AcceptsAnyValue(t *testing.T) {
	// Set never validates — all parsing happens in runRegister.
	// Confirm it tolerates empty strings and odd specs.
	var f bambuFlag
	if err := f.Set(""); err != nil {
		t.Errorf("Set(\"\") should not error, got %v", err)
	}
	if err := f.Set("only-one-field"); err != nil {
		t.Errorf("Set with no commas should not error, got %v", err)
	}
	if len(f) != 2 {
		t.Errorf("len = %d, want 2 (both appended)", len(f))
	}
}
