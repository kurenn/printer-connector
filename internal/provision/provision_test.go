package provision

import (
	"context"
	"errors"
	"testing"

	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
)

func TestParseBambu(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    config.Printer
		wantErr bool
	}{
		{
			name: "full spec",
			spec: "192.168.68.78,0300CA612001784,16515259,A1 mini",
			want: config.Printer{Type: config.TypeBambu, Name: "A1 mini", Host: "192.168.68.78",
				Serial: "0300CA612001784", AccessCode: "16515259"},
		},
		{
			// Name is optional — fall back to the host so the row is identifiable.
			name: "name omitted falls back to host",
			spec: "192.168.68.78,SERIAL,CODE",
			want: config.Printer{Type: config.TypeBambu, Name: "192.168.68.78", Host: "192.168.68.78",
				Serial: "SERIAL", AccessCode: "CODE"},
		},
		{
			name: "surrounding whitespace is trimmed",
			spec: " 10.0.0.5 , SER , CODE , Shop ",
			want: config.Printer{Type: config.TypeBambu, Name: "Shop", Host: "10.0.0.5",
				Serial: "SER", AccessCode: "CODE"},
		},
		// MQTT and FTPS both authenticate with serial + access code, so a printer
		// missing either could never connect — reject rather than half-add it.
		{name: "missing access code", spec: "10.0.0.5,SERIAL", wantErr: true},
		{name: "empty access code", spec: "10.0.0.5,SERIAL,", wantErr: true},
		{name: "empty serial", spec: "10.0.0.5,,CODE", wantErr: true},
		{name: "empty host", spec: ",SERIAL,CODE", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBambu(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tt.spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseMoonraker(t *testing.T) {
	tests := []struct {
		name        string
		spec        string
		wantBaseURL string
		wantName    string
		wantErr     bool
	}{
		{"host only uses the default port", "192.168.68.70", "http://192.168.68.70:7125", "192.168.68.70", false},
		{"explicit port", "192.168.68.70,7130", "http://192.168.68.70:7130", "192.168.68.70", false},
		{"port and name", "192.168.68.70,7125,voron", "http://192.168.68.70:7125", "voron", false},
		{"empty host", "", "", "", true},
		{"bad port", "10.0.0.5,notaport", "", "", true},
		{"out of range port", "10.0.0.5,70000", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMoonraker(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tt.spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.BaseURL != tt.wantBaseURL {
				t.Errorf("base_url = %q, want %q", got.BaseURL, tt.wantBaseURL)
			}
			if got.Name != tt.wantName {
				t.Errorf("name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Type != config.TypeMoonraker {
				t.Errorf("type = %q, want moonraker", got.Type)
			}
		})
	}
}

// A Bambu keeps its identity across a DHCP move, so it's keyed by serial rather
// than address.
func TestKeyIdentifiesPrintersAcrossAddressChanges(t *testing.T) {
	a := config.Printer{Type: config.TypeBambu, Serial: "SER", Host: "10.0.0.5"}
	b := config.Printer{Type: config.TypeBambu, Serial: "SER", Host: "10.0.0.99"} // moved
	if Key(a) != Key(b) {
		t.Errorf("same serial should key alike: %q vs %q", Key(a), Key(b))
	}
	m1 := config.Printer{Type: config.TypeMoonraker, BaseURL: "http://10.0.0.7:7125"}
	m2 := config.Printer{Type: config.TypeMoonraker, BaseURL: "http://10.0.0.7:7130"}
	if Key(m1) == Key(m2) {
		t.Error("different moonraker ports should key differently")
	}
}

func TestUnregisteredSkipsKnownAndDuplicates(t *testing.T) {
	existing := []config.Printer{
		{Type: config.TypeBambu, Serial: "OLD", Host: "10.0.0.1"},
		{Type: config.TypeMoonraker, BaseURL: "http://10.0.0.7:7125"},
	}
	candidates := []config.Printer{
		{Type: config.TypeBambu, Serial: "OLD", Host: "10.0.0.1"},     // already managed
		{Type: config.TypeMoonraker, BaseURL: "http://10.0.0.7:7125"}, // already managed
		{Type: config.TypeBambu, Serial: "NEW", Host: "10.0.0.2"},     // new
		{Type: config.TypeBambu, Serial: "NEW", Host: "10.0.0.2"},     // dup within the batch
	}
	got := Unregistered(existing, candidates)
	if len(got) != 1 {
		t.Fatalf("expected exactly the one new printer, got %d: %+v", len(got), got)
	}
	if got[0].Serial != "NEW" {
		t.Errorf("kept the wrong printer: %+v", got[0])
	}
}

type fakeRegistrar struct {
	gotConnectorID string
	gotInfos       []cloud.PrinterInfo
	adopted        []cloud.AdoptedPrinter
	err            error
}

func (f *fakeRegistrar) RegisterPrinters(_ context.Context, connectorID string, printers []cloud.PrinterInfo) ([]cloud.AdoptedPrinter, error) {
	f.gotConnectorID = connectorID
	f.gotInfos = printers
	return f.adopted, f.err
}

func TestAddAssignsIdsAndKeepsCredentialsLocal(t *testing.T) {
	reg := &fakeRegistrar{adopted: []cloud.AdoptedPrinter{{ID: 42, Name: "A1 mini"}}}
	in := []config.Printer{{Type: config.TypeBambu, Name: "A1 mini", Host: "10.0.0.5",
		Serial: "SER", AccessCode: "SECRET"}}

	out, err := Add(context.Background(), reg, "5", in)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(out) != 1 || out[0].PrinterID != 42 {
		t.Fatalf("expected the assigned id 42, got %+v", out)
	}
	if out[0].AccessCode != "SECRET" {
		t.Error("the local printer record should retain its access code")
	}
	if reg.gotConnectorID != "5" {
		t.Errorf("connector id = %q, want 5", reg.gotConnectorID)
	}
	// The access code must never leave the connector.
	if len(reg.gotInfos) != 1 {
		t.Fatalf("expected 1 printer sent, got %d", len(reg.gotInfos))
	}
	if reg.gotInfos[0].Type != config.TypeBambu || reg.gotInfos[0].Host != "10.0.0.5" {
		t.Errorf("unexpected payload: %+v", reg.gotInfos[0])
	}
}

// A printer the cloud declines (e.g. already owned by another workspace) must
// not be recorded locally, or the connector would poll a printer it can't push.
func TestAddDropsDeclinedPrinters(t *testing.T) {
	reg := &fakeRegistrar{adopted: []cloud.AdoptedPrinter{{ID: 7, Name: "kept"}}}
	in := []config.Printer{
		{Type: config.TypeBambu, Name: "kept", Host: "10.0.0.1", Serial: "A", AccessCode: "C"},
		{Type: config.TypeBambu, Name: "declined", Host: "10.0.0.2", Serial: "B", AccessCode: "C"},
	}
	out, err := Add(context.Background(), reg, "5", in)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(out) != 1 || out[0].Name != "kept" {
		t.Errorf("only the adopted printer should come back, got %+v", out)
	}
}

func TestAddRequiresAPairedConnector(t *testing.T) {
	_, err := Add(context.Background(), &fakeRegistrar{}, "", []config.Printer{{Name: "x"}})
	if err == nil {
		t.Fatal("expected an error when the connector isn't paired")
	}
}

func TestAddPropagatesCloudErrors(t *testing.T) {
	reg := &fakeRegistrar{err: errors.New("boom")}
	if _, err := Add(context.Background(), reg, "5", []config.Printer{{Name: "x"}}); err == nil {
		t.Fatal("expected the cloud error to propagate")
	}
}
