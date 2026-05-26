package moonraker

import (
	"context"
	"net/http/httptest"
	"testing"
)

// TestQueryObjectsIncludesThermalExtras verifies that when /printer/objects/list
// returns extra extruders and temperature_sensor objects (e.g. a chamber), they
// are included in the subsequent /printer/objects/query payload — and that
// unrelated objects (gcode_move) are not.
func TestQueryObjectsIncludesThermalExtras(t *testing.T) {
	t.Parallel()
	m := &mockMoonraker{objects: []string{
		"print_stats", "extruder", "heater_bed", "toolhead",
		"extruder1",
		"temperature_sensor chamber",
		"heater_generic chamber_heater",
		"temperature_fan mcu_fan",
		"gcode_move", // must NOT be pulled in
	}}
	srv := httptest.NewServer(m.handler(t))
	defer srv.Close()

	c := New(srv.URL, 0)
	out, err := c.QueryObjects(context.Background())
	if err != nil {
		t.Fatalf("QueryObjects: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, want := range []string{
		// always-queried core objects
		"print_stats", "virtual_sdcard", "extruder", "heater_bed", "toolhead", "pause_resume",
		// discovered thermal extras
		"extruder1",
		"temperature_sensor chamber",
		"heater_generic chamber_heater",
		"temperature_fan mcu_fan",
	} {
		if !m.lastQueryKeys[want] {
			t.Errorf("expected query to include %q; got keys %v", want, keys(m.lastQueryKeys))
		}
	}

	if m.lastQueryKeys["gcode_move"] {
		t.Errorf("non-thermal object gcode_move must not be queried")
	}

	// Confirm discovered objects appear in the returned status payload.
	status, _ := out["result"].(map[string]any)["status"].(map[string]any)
	for _, want := range []string{"extruder1", "temperature_sensor chamber"} {
		if _, ok := status[want]; !ok {
			t.Errorf("payload status missing %q: %v", want, status)
		}
	}
}

// TestQueryObjectsThermalCaching verifies that /printer/objects/list is called
// only once across multiple QueryObjects calls (the discovered names are cached).
func TestQueryObjectsThermalCaching(t *testing.T) {
	t.Parallel()
	m := &mockMoonraker{objects: []string{
		"extruder1", "temperature_sensor chamber",
	}}
	srv := httptest.NewServer(m.handler(t))
	defer srv.Close()

	c := New(srv.URL, 0)
	for i := 0; i < 3; i++ {
		if _, err := c.QueryObjects(context.Background()); err != nil {
			t.Fatalf("QueryObjects #%d: %v", i, err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listCalls != 1 {
		t.Errorf("expected objects/list called once (cached), got %d", m.listCalls)
	}
	if m.queryCalls != 3 {
		t.Errorf("expected 3 query calls, got %d", m.queryCalls)
	}
}

// TestQueryObjectsThermalListFailureRetries verifies that when /printer/objects/list
// fails, QueryObjects still succeeds (returns core objects only) and the discovery
// is retried on the next call rather than permanently skipped.
func TestQueryObjectsThermalListFailureRetries(t *testing.T) {
	t.Parallel()
	m := &mockMoonraker{
		objects:    []string{"extruder1", "temperature_sensor chamber"},
		listStatus: 500,
	}
	srv := httptest.NewServer(m.handler(t))
	defer srv.Close()

	c := New(srv.URL, 0)

	// First call: list fails → core objects only, no thermal extras.
	if _, err := c.QueryObjects(context.Background()); err != nil {
		t.Fatalf("QueryObjects should survive list failure: %v", err)
	}
	m.mu.Lock()
	if m.lastQueryKeys["extruder1"] {
		t.Error("extruder1 should be absent when discovery failed")
	}
	if m.lastQueryKeys["temperature_sensor chamber"] {
		t.Error("temperature_sensor chamber should be absent when discovery failed")
	}
	if !m.lastQueryKeys["print_stats"] {
		t.Error("core objects must still be queried when discovery fails")
	}
	m.listStatus = 0 // restore to 200
	m.mu.Unlock()

	// Second call: discovery retried (not permanently skipped) and now succeeds.
	if _, err := c.QueryObjects(context.Background()); err != nil {
		t.Fatalf("QueryObjects #2: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listCalls < 2 {
		t.Errorf("expected objects/list retried after failure, calls=%d", m.listCalls)
	}
	if !m.lastQueryKeys["extruder1"] {
		t.Error("extruder1 should be included after discovery recovers")
	}
	if !m.lastQueryKeys["temperature_sensor chamber"] {
		t.Error("temperature_sensor chamber should be included after discovery recovers")
	}
}

// TestIsThermalExtra confirms the classifier logic for each object category.
func TestIsThermalExtra(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want bool
	}{
		{"extruder1", true},
		{"extruder2", true},
		{"extruder10", true},
		{"extruder", false},  // primary extruder — always queried, not an "extra"
		{"extruderX", false}, // non-numeric suffix
		{"temperature_sensor chamber", true},
		{"temperature_sensor bed_outer", true},
		{"heater_generic chamber_heater", true},
		{"temperature_fan mcu_fan", true},
		{"temperature_fan", false}, // no space — not a named fan object
		{"gcode_move", false},
		{"print_stats", false},
		{"heater_bed", false},
		{"filament_switch_sensor runout", false}, // handled separately
	}
	for _, tc := range cases {
		got := isThermalExtra(tc.name)
		if got != tc.want {
			t.Errorf("isThermalExtra(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
