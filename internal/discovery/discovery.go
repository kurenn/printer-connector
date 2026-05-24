// Package discovery finds Moonraker-based 3D printers on the local network so a
// connector can adopt them without per-printer configuration. It uses only the
// standard library (no external deps): a parallel TCP sweep of the local /24(s)
// for the Moonraker port, confirmed by a GET /printer/info probe.
//
// mDNS/Bonjour is a deliberate non-goal for v1 — in the field many Klipper rigs
// don't advertise _moonraker._tcp, and the port sweep finds them reliably.
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

// DefaultPort is Moonraker's default HTTP API port.
const DefaultPort = 7125

// Printer is a discovered Moonraker instance.
type Printer struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	BaseURL  string `json:"base_url"`
	Hostname string `json:"hostname"`
	State    string `json:"state"`
}

// Options tune a scan. Zero values fall back to sensible defaults.
type Options struct {
	Port        int           // Moonraker port (default 7125)
	DialTimeout time.Duration // per-host TCP connect timeout (default 600ms)
	HTTPTimeout time.Duration // /printer/info probe timeout (default 2s)
	Workers     int           // concurrent probes (default 128)
	Hosts       []string      // override the host list (mainly for tests)
}

func (o *Options) withDefaults() {
	if o.Port == 0 {
		o.Port = DefaultPort
	}
	if o.DialTimeout == 0 {
		o.DialTimeout = 600 * time.Millisecond
	}
	if o.HTTPTimeout == 0 {
		o.HTTPTimeout = 2 * time.Second
	}
	if o.Workers <= 0 {
		o.Workers = 128
	}
}

// Scan probes the local network and returns the Moonraker printers it finds,
// sorted by IP. The host list comes from the machine's own IPv4 subnets unless
// Options.Hosts is provided.
func Scan(ctx context.Context, opts Options) ([]Printer, error) {
	opts.withDefaults()

	hosts := opts.Hosts
	if len(hosts) == 0 {
		var err error
		if hosts, err = LocalHostIPs(); err != nil {
			return nil, err
		}
	}

	client := &http.Client{Timeout: opts.HTTPTimeout}

	jobs := make(chan string)
	results := make(chan Printer)
	var wg sync.WaitGroup

	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				if p, ok := probe(ctx, client, ip, opts.Port, opts.DialTimeout); ok {
					results <- p
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, ip := range hosts {
			select {
			case <-ctx.Done():
				return
			case jobs <- ip:
			}
		}
	}()

	go func() { wg.Wait(); close(results) }()

	found := make([]Printer, 0)
	for p := range results {
		found = append(found, p)
	}
	sort.Slice(found, func(i, j int) bool { return less(found[i].IP, found[j].IP) })
	return found, nil
}

// probe checks one host: a quick TCP connect, then a Moonraker /printer/info GET.
func probe(ctx context.Context, client *http.Client, ip string, port int, dialTimeout time.Duration) (Printer, bool) {
	addr := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return Printer{}, false
	}
	_ = conn.Close()

	base := fmt.Sprintf("http://%s", addr)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/printer/info", nil)
	if err != nil {
		return Printer{}, false
	}
	resp, err := client.Do(req)
	if err != nil {
		// Port was open but not Moonraker (or unreachable) — skip.
		return Printer{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Printer{}, false
	}

	info, ok := ParsePrinterInfo(resp.Body)
	if !ok {
		return Printer{}, false
	}
	return Printer{
		IP:       ip,
		Port:     port,
		BaseURL:  base,
		Hostname: info.Hostname,
		State:    info.State,
	}, true
}

// PrinterInfo is the subset of Moonraker's /printer/info we care about.
type PrinterInfo struct {
	Hostname string
	State    string
}

// ParsePrinterInfo decodes a Moonraker /printer/info body. It returns ok=false
// when the payload doesn't look like Moonraker (so non-printer services on the
// port are ignored).
func ParsePrinterInfo(r interface{ Read([]byte) (int, error) }) (PrinterInfo, bool) {
	var body struct {
		Result *struct {
			Hostname string `json:"hostname"`
			State    string `json:"state"`
		} `json:"result"`
	}
	if err := json.NewDecoder(r).Decode(&body); err != nil || body.Result == nil {
		return PrinterInfo{}, false
	}
	return PrinterInfo{Hostname: body.Result.Hostname, State: body.Result.State}, true
}

// LocalHostIPs returns candidate host IPs from the machine's own IPv4 subnets.
// Subnets wider than /24 are clamped to the /24 surrounding the local address
// so a scan never explodes into thousands of hosts.
func LocalHostIPs() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	out := make([]string, 0, 256)

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			for _, h := range hostsForSubnet(ip4, ipnet.Mask) {
				if !seen[h] {
					seen[h] = true
					out = append(out, h)
				}
			}
		}
	}
	return out, nil
}

// hostsForSubnet enumerates host IPs for the /24 containing ip (clamping wider
// masks to /24), excluding the network and broadcast addresses.
func hostsForSubnet(ip net.IP, _ net.IPMask) []string {
	ip = ip.To4()
	if ip == nil {
		return nil
	}
	prefix := fmt.Sprintf("%d.%d.%d.", ip[0], ip[1], ip[2])
	hosts := make([]string, 0, 254)
	for i := 1; i <= 254; i++ {
		hosts = append(hosts, fmt.Sprintf("%s%d", prefix, i))
	}
	return hosts
}

// less compares two dotted-quad IPs numerically for stable sorting.
func less(a, b string) bool {
	ia, ib := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if ia == nil || ib == nil {
		return a < b
	}
	for i := 0; i < 4; i++ {
		if ia[i] != ib[i] {
			return ia[i] < ib[i]
		}
	}
	return false
}
