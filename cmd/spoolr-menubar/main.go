// Command spoolr-menubar is a small Tailscale-style menubar app: it runs the
// connector agent in-process and shows, at a glance, that the connector is
// running and which printers are reachable. GUI dependencies (systray) live
// only in this binary; the headless `connector` binary stays dependency-free.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"fyne.io/systray"

	"printer-connector/internal/agent"
	"printer-connector/internal/config"
	"printer-connector/internal/discovery"
)

var version = "0.1.0"

func main() {
	var (
		cfgPath  string
		check    bool
		logLevel string
	)
	flag.StringVar(&cfgPath, "config", "", "Path to connector config JSON (required)")
	flag.BoolVar(&check, "check", false, "Print status as text and exit (no GUI; for testing)")
	flag.StringVar(&logLevel, "log-level", "info", "Log level: debug|info|warn|error")
	flag.Parse()

	if cfgPath == "" {
		fmt.Fprintln(os.Stderr, "error: --config is required")
		os.Exit(2)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	// Headless status check — verifiable without a GUI session.
	if check {
		st := computeStatus(context.Background(), cfg)
		fmt.Printf("Connector: configured (%d printer(s)) · cloud %s\n", len(st.Printers), cfg.CloudURL)
		for _, p := range st.Printers {
			fmt.Printf("  %s %s — %s (%s)\n", dot(p.Online), p.Name, label(p.Online), p.BaseURL)
		}
		fmt.Printf("%d/%d online\n", st.OnlineCount(), len(st.Printers))
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(logLevel)}))
	app := &menubar{cfg: cfg, cfgPath: cfgPath, logger: logger}
	systray.Run(app.onReady, app.onExit)
}

// --- status (GUI-independent, unit-tested) ---

type printerStatus struct {
	Name    string
	BaseURL string
	Online  bool
}

type status struct {
	Printers []printerStatus
}

func (s status) OnlineCount() int {
	n := 0
	for _, p := range s.Printers {
		if p.Online {
			n++
		}
	}
	return n
}

// computeStatus probes each configured printer's Moonraker for reachability.
// It's intentionally cloud-independent: the menubar reflects local printer
// health even if the cloud is unreachable.
func computeStatus(ctx context.Context, cfg *config.Config) status {
	client := &http.Client{Timeout: 2 * time.Second}
	out := status{Printers: make([]printerStatus, 0, len(cfg.Printers))}
	for _, p := range cfg.Printers {
		out.Printers = append(out.Printers, printerStatus{
			Name:    printerName(p),
			BaseURL: p.BaseURL,
			Online:  probeOnline(ctx, client, p.BaseURL),
		})
	}
	return out
}

func probeOnline(ctx context.Context, client *http.Client, baseURL string) bool {
	if baseURL == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/printer/info", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	_, ok := discovery.ParsePrinterInfo(resp.Body)
	return ok
}

func printerName(p config.Printer) string {
	if p.Name != "" {
		return p.Name
	}
	if p.Host != "" {
		return p.Host
	}
	return p.BaseURL
}

func dot(online bool) string {
	if online {
		return "●"
	}
	return "○"
}

func label(online bool) string {
	if online {
		return "online"
	}
	return "offline"
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// --- menubar (systray) ---

type menubar struct {
	cfg          *config.Config
	cfgPath      string
	logger       *slog.Logger
	cancel       context.CancelFunc
	statusItem   *systray.MenuItem
	printerItems []*systray.MenuItem
}

func (m *menubar) onReady() {
	systray.SetTitle("Spoolr")
	systray.SetTooltip("Spoolr Connect")

	m.statusItem = systray.AddMenuItem("Starting…", "Connector status")
	m.statusItem.Disable()
	systray.AddSeparator()

	for _, p := range m.cfg.Printers {
		it := systray.AddMenuItem("○ "+printerName(p)+" — …", p.BaseURL)
		it.Disable()
		m.printerItems = append(m.printerItems, it)
	}

	systray.AddSeparator()
	openItem := systray.AddMenuItem("Open Spoolr", "Open the web app")
	quitItem := systray.AddMenuItem("Quit", "Stop the connector and quit")

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	// Run the connector agent in the background so this app *is* the connector.
	go func() {
		a := agent.New(agent.Options{ConfigPath: m.cfgPath, Config: m.cfg, Logger: m.logger, Version: version})
		if err := a.Run(ctx); err != nil && ctx.Err() == nil {
			m.logger.Warn("agent stopped", "error", err)
		}
	}()

	go m.refreshLoop(ctx)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-openItem.ClickedCh:
				openURL(m.cfg.CloudURL)
			case <-quitItem.ClickedCh:
				cancel()
				systray.Quit()
				return
			}
		}
	}()
}

func (m *menubar) onExit() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *menubar) refreshLoop(ctx context.Context) {
	m.refresh(ctx)
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			m.refresh(ctx)
		}
	}
}

func (m *menubar) refresh(ctx context.Context) {
	st := computeStatus(ctx, m.cfg)
	for i, p := range st.Printers {
		if i < len(m.printerItems) {
			m.printerItems[i].SetTitle(fmt.Sprintf("%s %s — %s", dot(p.Online), p.Name, label(p.Online)))
		}
	}
	m.statusItem.SetTitle(fmt.Sprintf("Connector running · %d/%d online", st.OnlineCount(), len(st.Printers)))
	systray.SetTooltip(fmt.Sprintf("Spoolr — %d/%d printers online", st.OnlineCount(), len(st.Printers)))
}

func openURL(url string) {
	if url == "" {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
