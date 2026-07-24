// Package provision adds printers to a connector that is already paired.
//
// Pairing is the auth boundary for a *new* connector: `register --token`
// exchanges a single-use token for connector credentials. Adding a printer to a
// connector that already holds those credentials is a different act — it needs
// no fresh user grant, and the cloud already exposes it as an authenticated
// endpoint (POST /api/v1/connectors/:id/printers), which the agent's periodic
// re-discovery has always used to adopt newly-found Moonraker printers.
//
// Bambu printers were the odd ones out only because their access code cannot be
// discovered — the user reads it off the printer's screen — so auto-adoption
// can't supply it. That is a UI gap, not a security one, and it forced the app
// to send users back for a pairing token just to add a printer. This package is
// the seam that closes it: parse a printer spec, skip what's already known,
// register the rest with the connector's own credentials.
package provision

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
)

// DefaultMoonrakerPort mirrors discovery's probe port.
const DefaultMoonrakerPort = 7125

// Registrar is the cloud seam. *cloud.Client satisfies it; tests inject a fake.
type Registrar interface {
	RegisterPrinters(ctx context.Context, connectorID string, printers []cloud.PrinterInfo) ([]cloud.AdoptedPrinter, error)
}

// ParseBambu reads a "host,serial,accesscode[,name]" spec. Serial and access
// code are both required — the MQTT and FTPS sessions authenticate with them,
// so a printer missing either could never connect.
func ParseBambu(spec string) (config.Printer, error) {
	parts := strings.Split(spec, ",")
	if len(parts) < 3 {
		return config.Printer{}, fmt.Errorf("bambu spec %q: want host,serial,accesscode[,name]", spec)
	}
	host := strings.TrimSpace(parts[0])
	serial := strings.TrimSpace(parts[1])
	code := strings.TrimSpace(parts[2])
	if host == "" || serial == "" || code == "" {
		return config.Printer{}, fmt.Errorf("bambu spec %q: host, serial and accesscode are all required", spec)
	}
	name := host
	if len(parts) > 3 && strings.TrimSpace(parts[3]) != "" {
		name = strings.TrimSpace(parts[3])
	}
	return config.Printer{
		Type:       config.TypeBambu,
		Name:       name,
		Host:       host,
		Serial:     serial,
		AccessCode: code,
	}, nil
}

// ParseMoonraker reads a "host[,port][,name]" spec. Moonraker needs no
// credentials, so a host alone is enough.
func ParseMoonraker(spec string) (config.Printer, error) {
	parts := strings.Split(spec, ",")
	host := strings.TrimSpace(parts[0])
	if host == "" {
		return config.Printer{}, fmt.Errorf("moonraker spec %q: host is required", spec)
	}
	port := DefaultMoonrakerPort
	if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
		p, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || p <= 0 || p > 65535 {
			return config.Printer{}, fmt.Errorf("moonraker spec %q: bad port %q", spec, parts[1])
		}
		port = p
	}
	name := host
	if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
		name = strings.TrimSpace(parts[2])
	}
	return config.Printer{
		Type:    config.TypeMoonraker,
		Name:    name,
		BaseURL: fmt.Sprintf("http://%s:%d", host, port),
		UIPort:  80,
	}, nil
}

// Key identifies a printer for de-duplication. Bambu printers are keyed by
// serial (stable across DHCP moves); Moonraker printers by host:port, matching
// how the agent's re-discovery already de-dupes.
func Key(p config.Printer) string {
	if p.Type == config.TypeBambu {
		if p.Serial != "" {
			return "bambu:" + strings.ToLower(p.Serial)
		}
		return "bambu:" + strings.ToLower(p.Host)
	}
	host, port := moonrakerHostPort(p)
	if host == "" {
		return ""
	}
	return "moonraker:" + net.JoinHostPort(strings.ToLower(host), strconv.Itoa(port))
}

// Unregistered returns the candidates not already present in existing, also
// collapsing duplicates within candidates itself.
func Unregistered(existing, candidates []config.Printer) []config.Printer {
	known := make(map[string]bool, len(existing))
	for _, p := range existing {
		if k := Key(p); k != "" {
			known[k] = true
		}
	}
	out := make([]config.Printer, 0, len(candidates))
	for _, c := range candidates {
		k := Key(c)
		if k == "" || known[k] {
			continue
		}
		known[k] = true
		out = append(out, c)
	}
	return out
}

// Add registers printers with the cloud using the connector's own credentials
// and returns them with their assigned printer ids. Printers the cloud declined
// (e.g. already owned by another workspace) are omitted, so callers never
// persist a printer the cloud doesn't know about.
func Add(ctx context.Context, reg Registrar, connectorID string, printers []config.Printer) ([]config.Printer, error) {
	if connectorID == "" {
		return nil, errors.New("provision: connector is not paired")
	}
	if len(printers) == 0 {
		return nil, nil
	}

	infos := make([]cloud.PrinterInfo, 0, len(printers))
	for _, p := range printers {
		infos = append(infos, printerInfo(p))
	}

	adopted, err := reg.RegisterPrinters(ctx, connectorID, infos)
	if err != nil {
		return nil, err
	}

	// Map assigned ids back by name, the same correlation `register` uses.
	byName := make(map[string]int, len(adopted))
	for _, a := range adopted {
		byName[a.Name] = a.ID
	}
	out := make([]config.Printer, 0, len(printers))
	for _, p := range printers {
		id, ok := byName[p.Name]
		if !ok {
			continue // cloud declined it — don't record it locally
		}
		p.PrinterID = id
		out = append(out, p)
	}
	return out, nil
}

// printerInfo is the cloud-facing view. Bambu credentials are deliberately not
// included — they stay on the connector.
func printerInfo(p config.Printer) cloud.PrinterInfo {
	if p.Type == config.TypeBambu {
		return cloud.PrinterInfo{Name: p.Name, Type: config.TypeBambu, Host: p.Host}
	}
	host, port := moonrakerHostPort(p)
	return cloud.PrinterInfo{
		Name:          p.Name,
		Type:          config.TypeMoonraker,
		Host:          host,
		MoonrakerPort: port,
		UIPort:        p.UIPort,
	}
}

// moonrakerHostPort pulls host and port out of a Moonraker printer's BaseURL.
func moonrakerHostPort(p config.Printer) (string, int) {
	if p.BaseURL == "" {
		return "", 0
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Host == "" {
		return "", 0
	}
	host := u.Hostname()
	port := DefaultMoonrakerPort
	if ps := u.Port(); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil {
			port = n
		}
	}
	return host, port
}
