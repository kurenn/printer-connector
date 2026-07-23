package discovery

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
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

// Discovery mechanism that surfaced a printer. Recorded so a support question
// like "why didn't my printer show up?" is answerable from the scan output.
const (
	SourceSSDP = "ssdp"
	SourceTLS  = "tls"
)

// BambuPrinter is a Bambu device seen on the LAN. AccessCode is intentionally
// absent — discovery can't learn it.
type BambuPrinter struct {
	Host   string `json:"host"`
	Serial string `json:"serial"`
	Model  string `json:"model"`
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
}

// ScanBambu listens for Bambu SSDP beacons for the given duration and returns
// the unique printers seen.
//
// The error is returned rather than swallowed: the common failure is a slicer
// already holding UDP :2021, which is permanent for the life of that process
// and would otherwise look identical to "no printers on this network". Callers
// pair this with ScanBambuTLS, which has no such conflict.
func ScanBambu(ctx context.Context, listen time.Duration) ([]BambuPrinter, error) {
	addr := &net.UDPAddr{IP: net.ParseIP(bambuSSDPGroup), Port: bambuSSDPPort}
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("bambu ssdp: cannot listen on %s:%d (a slicer such as Bambu Studio or Orca may hold it): %w", bambuSSDPGroup, bambuSSDPPort, err)
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
	return out, nil
}

// DiscoverBambu finds Bambu printers by both mechanisms and merges the results.
// SSDP is richer (it carries model and friendly name) but is often unavailable;
// the TLS sweep always works but only yields host and serial. Running both and
// preferring SSDP's details gives the best record of each printer.
//
// An SSDP failure is reported, not fatal — the TLS sweep still finds printers,
// so the error is a diagnostic rather than a reason to return nothing.
func DiscoverBambu(ctx context.Context, listen time.Duration) ([]BambuPrinter, error) {
	var (
		ssdp, viaTLS []BambuPrinter
		ssdpErr      error
		wg           sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); ssdp, ssdpErr = ScanBambu(ctx, listen) }()
	go func() { defer wg.Done(); viaTLS = ScanBambuTLS(ctx) }()
	wg.Wait()

	// Key by serial: the same printer found both ways must appear once.
	merged := make(map[string]BambuPrinter, len(ssdp)+len(viaTLS))
	for _, p := range viaTLS {
		merged[p.Serial] = p
	}
	for _, p := range ssdp {
		p.Source = SourceSSDP
		if prev, ok := merged[p.Serial]; ok && p.Host == "" {
			p.Host = prev.Host
		}
		merged[p.Serial] = p // SSDP wins: it carries model and friendly name
	}

	out := make([]BambuPrinter, 0, len(merged))
	for _, p := range merged {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out, ssdpErr
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
