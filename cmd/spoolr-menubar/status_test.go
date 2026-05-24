package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"printer-connector/internal/config"
)

func TestComputeStatus(t *testing.T) {
	// A reachable Moonraker stub…
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/printer/info" {
			_, _ = w.Write([]byte(`{"result":{"state":"ready","hostname":"k1"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Printers: []config.Printer{
			{Name: "K1Max-1814", BaseURL: srv.URL},
			{Name: "viktor", BaseURL: "http://127.0.0.1:1"}, // nothing listening
		},
	}

	st := computeStatus(context.Background(), cfg)
	if len(st.Printers) != 2 {
		t.Fatalf("expected 2 printers, got %d", len(st.Printers))
	}
	if !st.Printers[0].Online {
		t.Error("expected K1Max-1814 to be online")
	}
	if st.Printers[1].Online {
		t.Error("expected viktor to be offline")
	}
	if st.OnlineCount() != 1 {
		t.Errorf("expected OnlineCount 1, got %d", st.OnlineCount())
	}
}

func TestPrinterName(t *testing.T) {
	if got := printerName(config.Printer{Name: "K1"}); got != "K1" {
		t.Errorf("name: got %q", got)
	}
	if got := printerName(config.Printer{Host: "192.168.1.5"}); got != "192.168.1.5" {
		t.Errorf("host fallback: got %q", got)
	}
}
