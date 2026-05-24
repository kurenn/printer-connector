package discovery

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestParsePrinterInfo(t *testing.T) {
	info, ok := ParsePrinterInfo(strings.NewReader(`{"result":{"state":"ready","hostname":"K1Max-1814"}}`))
	if !ok || info.Hostname != "K1Max-1814" || info.State != "ready" {
		t.Fatalf("got %+v ok=%v", info, ok)
	}

	if _, ok := ParsePrinterInfo(strings.NewReader(`{"something":1}`)); ok {
		t.Fatal("expected a non-Moonraker payload to be rejected")
	}
}

func TestScanFindsMoonraker(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/printer/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"state":"ready","hostname":"test-printer"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	port, _ := strconv.Atoi(portStr)

	found, err := Scan(context.Background(), Options{Hosts: []string{host}, Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 printer, got %d: %+v", len(found), found)
	}
	if found[0].Hostname != "test-printer" || found[0].State != "ready" || found[0].Port != port {
		t.Fatalf("unexpected printer: %+v", found[0])
	}
}

func TestScanIgnoresNonMoonraker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello, not a printer"))
	}))
	defer srv.Close()

	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	port, _ := strconv.Atoi(portStr)

	found, err := Scan(context.Background(), Options{Hosts: []string{host}, Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("expected 0 printers (non-Moonraker service), got %+v", found)
	}
}

func TestHostsForSubnetClampsToSlash24(t *testing.T) {
	hosts := hostsForSubnet(net.ParseIP("192.168.68.65"), net.CIDRMask(16, 32))
	if len(hosts) != 254 {
		t.Fatalf("expected 254 hosts, got %d", len(hosts))
	}
	if hosts[0] != "192.168.68.1" || hosts[253] != "192.168.68.254" {
		t.Fatalf("unexpected host range: %s .. %s", hosts[0], hosts[253])
	}
}
