package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
	"printer-connector/internal/discovery"
)

func knownPrinter() config.Printer {
	return config.Printer{PrinterID: 7, Type: config.TypeMoonraker, Name: "viktor", BaseURL: "http://192.168.1.81:7125", UIPort: 80}
}

func newAgent(t *testing.T, srvURL string) *Agent {
	t.Helper()
	return &Agent{
		log:     discardLogger(),
		cfgPath: filepath.Join(t.TempDir(), "config.json"),
		cfg: &config.Config{
			ConnectorID: "1",
			CloudURL:    srvURL,
			Printers:    []config.Printer{knownPrinter()},
		},
		drivers: buildDrivers([]config.Printer{knownPrinter()}),
		cloud:   cloud.New(cloud.Options{BaseURL: srvURL, ConnectorID: "1", ConnectorSecret: "s", Logger: discardLogger()}),
	}
}

// TestAdoptDiscovered_AdoptsOnlyNew verifies a printer that came online after
// pairing is adopted (driver + config + persisted), while an already-managed one
// is not re-sent.
func TestAdoptDiscovered_AdoptsOnlyNew(t *testing.T) {
	var (
		mu       sync.Mutex
		gotReq   cloud.RegisterPrintersRequest
		reqCount int
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/connectors/1/printers", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqCount++
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotReq)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cloud.RegisterPrintersResponse{
			Printers: []cloud.AdoptedPrinter{{ID: 42, Name: "marcus", Host: "192.168.1.50", MoonrakerPort: 7125}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := newAgent(t, srv.URL)
	found := []discovery.Printer{
		{Host: "192.168.1.81", Port: 7125, Name: "viktor", Kind: "klipper"}, // already managed
		{Host: "192.168.1.50", Port: 7125, Name: "marcus", Kind: "klipper"}, // new
	}

	if err := a.adoptDiscovered(context.Background(), found); err != nil {
		t.Fatalf("adoptDiscovered: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if reqCount != 1 {
		t.Fatalf("expected exactly 1 adopt request, got %d", reqCount)
	}
	if len(gotReq.Printers) != 1 || gotReq.Printers[0].Host != "192.168.1.50" {
		t.Errorf("expected only the new printer to be sent, got %+v", gotReq.Printers)
	}
	if a.driverFor(42) == nil {
		t.Error("expected a driver for the adopted printer id 42")
	}
	if len(a.managedPrinters()) != 2 {
		t.Errorf("expected 2 managed printers, got %d", len(a.managedPrinters()))
	}
	if data, _ := os.ReadFile(a.cfgPath); !strings.Contains(string(data), "192.168.1.50") {
		t.Error("expected the adopted printer to be persisted to the config file")
	}
}

// TestAdoptDiscovered_NoNewPrinters: when everything discovered is already
// managed, the connector must not call the adopt endpoint at all.
func TestAdoptDiscovered_NoNewPrinters(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/connectors/1/printers", func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = io.WriteString(w, `{"printers":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := newAgent(t, srv.URL)
	found := []discovery.Printer{{Host: "192.168.1.81", Port: 7125, Name: "viktor", Kind: "klipper"}}

	if err := a.adoptDiscovered(context.Background(), found); err != nil {
		t.Fatalf("adoptDiscovered: %v", err)
	}
	if called {
		t.Error("adopt endpoint should not be called when nothing new is discovered")
	}
	if len(a.managedPrinters()) != 1 {
		t.Errorf("managed printer set should be unchanged, got %d", len(a.managedPrinters()))
	}
}

// TestAdoptDiscovered_CloudError: a failing adopt endpoint surfaces the error and
// leaves the managed set untouched (no half-applied state).
func TestAdoptDiscovered_CloudError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := newAgent(t, srv.URL)
	found := []discovery.Printer{{Host: "192.168.1.50", Port: 7125, Name: "marcus", Kind: "klipper"}}

	if err := a.adoptDiscovered(context.Background(), found); err == nil {
		t.Fatal("expected an error when the adopt endpoint fails")
	}
	if len(a.managedPrinters()) != 1 {
		t.Errorf("managed set must be unchanged on error, got %d", len(a.managedPrinters()))
	}
}

// TestAdoptDiscovered_ConcurrentReads exercises the mutex: adoption mutates the
// driver map + printer list while the loops read them. Run with -race.
func TestAdoptDiscovered_ConcurrentReads(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/connectors/1/printers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(cloud.RegisterPrintersResponse{
			Printers: []cloud.AdoptedPrinter{{ID: 42, Name: "marcus", Host: "192.168.1.50", MoonrakerPort: 7125}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := newAgent(t, srv.URL)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = a.driverFor(7)
					_ = a.managedPrinters()
				}
			}
		}()
	}

	found := []discovery.Printer{{Host: "192.168.1.50", Port: 7125, Name: "marcus", Kind: "klipper"}}
	if err := a.adoptDiscovered(context.Background(), found); err != nil {
		t.Fatalf("adoptDiscovered: %v", err)
	}
	close(stop)
	wg.Wait()
}
