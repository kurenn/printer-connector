// Package discovery performs a LAN sweep for Moonraker printers. It is
// intentionally self-contained (no agent/config deps) so the `discover`
// subcommand can be invoked standalone — e.g. by the macOS menubar app to
// power its "Scan network" flow.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"
)

// MoonrakerPort is the default Moonraker API port probed during discovery.
const MoonrakerPort = 7125

// Printer is a discovered Moonraker instance.
type Printer struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Name   string `json:"name"`
	Kind   string `json:"kind"` // klipper | bambu | printer
	Detail string `json:"detail"`
}

// Result is the JSON payload emitted by `connector discover`.
type Result struct {
	HostsTotal  int       `json:"hosts_total"`
	HostsProbed int       `json:"hosts_probed"`
	Subnets     []string  `json:"subnets"`
	Printers    []Printer `json:"printers"`
}

// Scan sweeps every local /24 for an open Moonraker port, then confirms each
// hit with GET /printer/info. Bounded concurrency keeps a /24 to ~1–2s.
func Scan(ctx context.Context) Result {
	subnets := localSubnets()
	targets := make([]string, 0, len(subnets)*254)
	for _, base := range subnets {
		for i := 1; i <= 254; i++ {
			targets = append(targets, fmt.Sprintf("%s.%d", base, i))
		}
	}

	res := Result{HostsTotal: len(targets), Subnets: subnets}
	// Generous HTTP timeout so a busy printer (mid-print, slow to answer
	// /printer/info) is still caught rather than skipped.
	client := &http.Client{Timeout: 3 * time.Second}

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		probed int
		// Lower concurrency avoids congesting small home networks (dropped SYNs
		// → missed printers); a printer answers either way.
		sem = make(chan struct{}, 64)
	)

	for _, ip := range targets {
		select {
		case <-ctx.Done():
			wg.Wait()
			res.HostsProbed = probed
			return finalize(res)
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			mu.Lock()
			probed++
			mu.Unlock()
			// Generous TCP accept window so a busy/slow printer isn't skipped
			// before we even try its HTTP API.
			if !tcpOpen(ip, MoonrakerPort, 800*time.Millisecond) {
				return
			}
			if p, ok := probeMoonraker(ctx, client, ip, MoonrakerPort); ok {
				mu.Lock()
				res.Printers = append(res.Printers, p)
				mu.Unlock()
			}
		}(ip)
	}
	wg.Wait()
	res.HostsProbed = probed
	return finalize(res)
}

func finalize(res Result) Result {
	sort.Slice(res.Printers, func(i, j int) bool { return res.Printers[i].Host < res.Printers[j].Host })
	return res
}

// localSubnets returns the unique "a.b.c" prefixes of every up, non-loopback
// IPv4 interface.
func localSubnets() []string {
	out := []string{}
	seen := map[string]bool{}
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			base := fmt.Sprintf("%d.%d.%d", ip4[0], ip4[1], ip4[2])
			if !seen[base] {
				seen[base] = true
				out = append(out, base)
			}
		}
	}
	return out
}

func tcpOpen(ip string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ip, port), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func probeMoonraker(ctx context.Context, client *http.Client, ip string, port int) (Printer, bool) {
	url := fmt.Sprintf("http://%s:%d/printer/info", ip, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Printer{}, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return Printer{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Printer{}, false
	}
	var body struct {
		Result struct {
			Hostname string `json:"hostname"`
		} `json:"result"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	name := body.Result.Hostname
	if name == "" {
		name = ip
	}
	return Printer{
		Host:   ip,
		Port:   port,
		Name:   name,
		Kind:   "klipper",
		Detail: fmt.Sprintf("Klipper · Moonraker · %s", ip),
	}, true
}
