package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
	"printer-connector/internal/driver"
	"printer-connector/internal/moonraker"
)

// TestExecuteFetchGcode_EndToEnd drives pollAndExecuteCommands through a
// fetch_gcode command: the connector pulls the active print's G-code from a
// fake Moonraker file API and PUTs it to the presigned upload URL, then reports
// the command succeeded. A single httptest server stands in for both Moonraker
// and the cloud (routed by path).
func TestExecuteFetchGcode_EndToEnd(t *testing.T) {
	const gcodeBody = "G28\nG1 Z0.2 F300\nG1 X10 Y10 E1 F1500\n"

	var (
		mu             sync.Mutex
		commandsServed bool
		uploadedBody   string
		uploadedToken  string
		completed      cloud.CommandCompleteRequest
		completeSeen   bool
	)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// --- cloud: hand out exactly one fetch_gcode command, once ---
	mux.HandleFunc("/api/v1/connectors/1/commands", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if commandsServed {
			_, _ = io.WriteString(w, `[]`)
			return
		}
		commandsServed = true
		uploadURL := srv.URL + "/api/v1/gcode_caches/55/upload?token=signed-token"
		cmd := []map[string]any{{
			"id":         "9001",
			"printer_id": 1,
			"action":     "fetch_gcode",
			"params": map[string]any{
				"gcode_cache_id": "55",
				"filename":       "benchy.gcode",
				"upload_url":     uploadURL,
			},
		}}
		_ = json.NewEncoder(w).Encode(cmd)
	})

	// --- cloud: record the command completion ---
	mux.HandleFunc("/api/v1/commands/9001/complete", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		completeSeen = true
		_ = json.NewDecoder(r.Body).Decode(&completed)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	})

	// --- cloud: presigned upload target ---
	mux.HandleFunc("/api/v1/gcode_caches/55/upload", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		uploadedBody = string(body)
		uploadedToken = r.URL.Query().Get("token")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})

	// --- cloud: post-command snapshot push ---
	mux.HandleFunc("/api/v1/snapshots/batch", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"inserted":1}`)
	})

	// --- moonraker: serve the gcode file + the post-snapshot objects query ---
	mux.HandleFunc("/server/files/gcodes/benchy.gcode", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, gcodeBody)
	})
	mux.HandleFunc("/printer/objects/query", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":{"status":{"print_stats":{"state":"printing"}}}}`)
	})

	a := &Agent{
		log:     discardLogger(),
		cfg:     &config.Config{ConnectorID: "1", Printers: []config.Printer{{PrinterID: 1, BaseURL: srv.URL}}},
		drivers: map[int]driver.Driver{1: moonraker.New(srv.URL, 80)},
		cloud:   cloud.New(cloud.Options{BaseURL: srv.URL, ConnectorID: "1", Logger: discardLogger()}),
	}

	if err := a.pollAndExecuteCommands(context.Background()); err != nil {
		t.Fatalf("pollAndExecuteCommands: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if uploadedBody != gcodeBody {
		t.Errorf("uploaded body = %q, want the gcode bytes", uploadedBody)
	}
	if uploadedToken != "signed-token" {
		t.Errorf("upload token = %q, want signed-token (presigned URL must be used verbatim)", uploadedToken)
	}
	if !completeSeen {
		t.Fatal("command completion was never reported")
	}
	if completed.Status != "succeeded" {
		t.Errorf("command status = %q, want succeeded (err=%q)", completed.Status, completed.ErrorMessage)
	}
	if completed.Result["filename"] != "benchy.gcode" {
		t.Errorf("result.filename = %v, want benchy.gcode", completed.Result["filename"])
	}
}

// TestExecuteFetchGcode_RejectsMissingParams ensures the handler fails cleanly
// (rather than panicking or uploading garbage) when required params are absent.
func TestExecuteFetchGcode_RejectsMissingParams(t *testing.T) {
	a := &Agent{
		log:     discardLogger(),
		drivers: map[int]driver.Driver{1: moonraker.New("http://127.0.0.1:1", 80)},
	}

	cases := []map[string]any{
		{"upload_url": "http://x/up"}, // no filename
		{"filename": "benchy.gcode"},  // no upload_url
		{},                            // neither
	}
	for i, params := range cases {
		cmd := cloud.Command{ID: cloud.StringOrNumber(fmt.Sprint(i)), PrinterID: 1, Action: "fetch_gcode", Params: params}
		if err := a.executeFetchGcode(context.Background(), cmd, map[string]any{}); err == nil {
			t.Errorf("case %d: expected error for params %v, got nil", i, params)
		}
	}
}
