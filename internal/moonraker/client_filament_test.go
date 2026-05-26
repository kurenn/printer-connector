package moonraker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// mockMoonraker serves /printer/objects/list and /printer/objects/query and
// records how many times each is hit plus the object keys requested in the query.
type mockMoonraker struct {
	mu            sync.Mutex
	listCalls     int
	queryCalls    int
	lastQueryKeys map[string]bool
	listStatus    int      // status to return for objects/list (default 200)
	objects       []string // object names objects/list advertises
}

func (m *mockMoonraker) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch r.URL.Path {
		case "/printer/objects/list":
			m.listCalls++
			if m.listStatus != 0 && m.listStatus != http.StatusOK {
				w.WriteHeader(m.listStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{"objects": m.objects},
			})
		case "/printer/objects/query":
			m.queryCalls++
			var body struct {
				Objects map[string]any `json:"objects"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode query body: %v", err)
			}
			m.lastQueryKeys = map[string]bool{}
			status := map[string]any{}
			for k := range body.Objects {
				m.lastQueryKeys[k] = true
				status[k] = map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{"status": status},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestQueryObjectsIncludesDiscoveredFilamentSensors(t *testing.T) {
	t.Parallel()
	m := &mockMoonraker{objects: []string{
		"print_stats", "extruder", "gcode_move",
		"filament_switch_sensor runout", "filament_motion_sensor motion",
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
		"print_stats", "virtual_sdcard", "extruder", "heater_bed", "toolhead", "pause_resume",
		"filament_switch_sensor runout", "filament_motion_sensor motion",
	} {
		if !m.lastQueryKeys[want] {
			t.Errorf("expected query to include object %q; got keys %v", want, keys(m.lastQueryKeys))
		}
	}
	// non-filament objects must not be pulled in by the discovery filter
	if m.lastQueryKeys["gcode_move"] {
		t.Errorf("did not expect non-filament object gcode_move to be queried")
	}

	status, _ := out["result"].(map[string]any)["status"].(map[string]any)
	if _, ok := status["filament_switch_sensor runout"]; !ok {
		t.Errorf("snapshot payload status missing the filament sensor: %v", status)
	}
}

func TestQueryObjectsCachesSensorDiscovery(t *testing.T) {
	t.Parallel()
	m := &mockMoonraker{objects: []string{"print_stats", "filament_switch_sensor runout"}}
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
		t.Errorf("expected objects/list to be called once (cached), got %d", m.listCalls)
	}
	if m.queryCalls != 3 {
		t.Errorf("expected 3 query calls, got %d", m.queryCalls)
	}
}

func TestQueryObjectsSurvivesListFailureAndRetries(t *testing.T) {
	t.Parallel()
	m := &mockMoonraker{objects: []string{"filament_switch_sensor runout"}, listStatus: http.StatusInternalServerError}
	srv := httptest.NewServer(m.handler(t))
	defer srv.Close()

	c := New(srv.URL, 0)
	// First query: list fails -> still returns core objects, no sensor.
	if _, err := c.QueryObjects(context.Background()); err != nil {
		t.Fatalf("QueryObjects should survive list failure: %v", err)
	}
	m.mu.Lock()
	if m.lastQueryKeys["filament_switch_sensor runout"] {
		t.Errorf("sensor should be absent when discovery failed")
	}
	if !m.lastQueryKeys["print_stats"] {
		t.Errorf("core objects must still be queried when discovery fails")
	}
	m.listStatus = http.StatusOK // discovery recovers
	m.mu.Unlock()

	// Second query: discovery is retried (not cached on failure) and now succeeds.
	if _, err := c.QueryObjects(context.Background()); err != nil {
		t.Fatalf("QueryObjects #2: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listCalls < 2 {
		t.Errorf("expected objects/list retried after failure, calls=%d", m.listCalls)
	}
	if !m.lastQueryKeys["filament_switch_sensor runout"] {
		t.Errorf("sensor should be included after discovery recovers")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
