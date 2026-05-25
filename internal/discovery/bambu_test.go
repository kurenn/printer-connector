package discovery

import "testing"

func TestParseBambuBeacon(t *testing.T) {
	beacon := "NOTIFY * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:2021\r\n" +
		"Server: UPnP/1.0\r\n" +
		"Location: 192.168.1.50\r\n" +
		"NT: urn:bambulab-com:device:3dprinter:1\r\n" +
		"USN: 01S00A1234567890\r\n" +
		"DevModel.bambu.com: BL-P001\r\n" +
		"DevName.bambu.com: Shop P1S\r\n" +
		"DevConnect.bambu.com: lan\r\n"

	p, ok := parseBambuBeacon([]byte(beacon), "10.0.0.9")
	if !ok {
		t.Fatal("expected a Bambu printer to parse")
	}
	if p.Serial != "01S00A1234567890" {
		t.Errorf("serial = %q", p.Serial)
	}
	if p.Host != "192.168.1.50" { // Location wins over srcIP
		t.Errorf("host = %q", p.Host)
	}
	if p.Model != "BL-P001" {
		t.Errorf("model = %q", p.Model)
	}
	if p.Name != "Shop P1S" {
		t.Errorf("name = %q", p.Name)
	}
}

func TestParseBambuBeacon_FallsBackToSourceIP(t *testing.T) {
	beacon := "NOTIFY * HTTP/1.1\r\nUSN: ABC123\r\nDevModel.bambu.com: X1C\r\nbambulab\r\n"
	p, ok := parseBambuBeacon([]byte(beacon), "10.0.0.42")
	if !ok {
		t.Fatal("expected parse")
	}
	if p.Host != "10.0.0.42" {
		t.Errorf("host should fall back to source IP, got %q", p.Host)
	}
	if p.Name != "Bambu X1C" {
		t.Errorf("name should default from model, got %q", p.Name)
	}
}

func TestParseBambuBeacon_IgnoresNonBambu(t *testing.T) {
	// A generic SSDP NOTIFY (e.g. a router) must be ignored.
	beacon := "NOTIFY * HTTP/1.1\r\nUSN: uuid:abc\r\nNT: upnp:rootdevice\r\n"
	if _, ok := parseBambuBeacon([]byte(beacon), "10.0.0.1"); ok {
		t.Error("non-Bambu SSDP should not parse as a printer")
	}
}
