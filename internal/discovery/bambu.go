package discovery

import (
	"context"
	"net"
	"strings"
	"time"
)

// Bambu Lab printers announce themselves over SSDP on the LAN — periodic NOTIFY
// datagrams to 239.255.255.250:2021. Unlike Moonraker they speak MQTT+FTPS (no
// :7125 HTTP), so the Moonraker sweep can't see them; we listen for their SSDP
// beacons instead. The LAN access code is NOT in the beacon (it's shown on the
// printer's screen) — the user enters it during onboarding.

const (
	bambuSSDPGroup = "239.255.255.250"
	bambuSSDPPort  = 2021
)

// BambuPrinter is a Bambu device seen on the LAN. AccessCode is intentionally
// absent — discovery can't learn it.
type BambuPrinter struct {
	Host   string `json:"host"`
	Serial string `json:"serial"`
	Model  string `json:"model"`
	Name   string `json:"name"`
}

// ScanBambu listens for Bambu SSDP beacons for the given duration and returns
// the unique printers seen. Best-effort: returns nil (no error) if the socket
// can't be opened, so it composes cleanly alongside the Moonraker sweep.
func ScanBambu(ctx context.Context, listen time.Duration) []BambuPrinter {
	addr := &net.UDPAddr{IP: net.ParseIP(bambuSSDPGroup), Port: bambuSSDPPort}
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return nil
	}
	defer conn.Close()

	deadline := time.Now().Add(listen)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetReadBuffer(1 << 20)

	seen := map[string]BambuPrinter{}
	buf := make([]byte, 2048)
	for {
		if ctx.Err() != nil {
			break
		}
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			break // deadline reached or socket closed
		}
		if p, ok := parseBambuBeacon(buf[:n], src.IP.String()); ok {
			seen[p.Serial] = p
		}
	}

	out := make([]BambuPrinter, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	return out
}

// parseBambuBeacon parses an SSDP NOTIFY datagram from a Bambu printer. It is
// pure (no I/O) so it can be unit-tested. srcIP is the datagram's source, used
// as the host when the beacon omits a Location.
func parseBambuBeacon(data []byte, srcIP string) (BambuPrinter, bool) {
	text := string(data)
	// Only NOTIFY/200 SSDP messages, and only Bambu devices.
	upper := strings.ToUpper(text)
	if !strings.HasPrefix(upper, "NOTIFY") && !strings.HasPrefix(upper, "HTTP/1.1 200") {
		return BambuPrinter{}, false
	}
	if !strings.Contains(strings.ToLower(text), "bambulab") {
		return BambuPrinter{}, false
	}

	p := BambuPrinter{Host: srcIP}
	for _, line := range strings.Split(text, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch {
		case key == "usn":
			p.Serial = val
		case key == "location":
			if h := hostFromLocation(val); h != "" {
				p.Host = h
			}
		case strings.HasPrefix(key, "devmodel"):
			p.Model = val
		case strings.HasPrefix(key, "devname"):
			p.Name = val
		}
	}
	if p.Serial == "" || p.Host == "" {
		return BambuPrinter{}, false
	}
	if p.Name == "" {
		p.Name = "Bambu " + p.Model
	}
	return p, true
}

// hostFromLocation pulls the bare host out of a Location value, which may be a
// raw IP or a URL like "http://192.168.1.5".
func hostFromLocation(loc string) string {
	loc = strings.TrimSpace(loc)
	loc = strings.TrimPrefix(loc, "http://")
	loc = strings.TrimPrefix(loc, "https://")
	if i := strings.IndexAny(loc, ":/"); i >= 0 {
		loc = loc[:i]
	}
	if net.ParseIP(loc) != nil || loc != "" {
		return loc
	}
	return ""
}
