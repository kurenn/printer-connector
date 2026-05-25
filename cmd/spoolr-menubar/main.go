// Command spoolr-menubar is a small Tailscale-style menubar app: it runs the
// connector agent in-process and shows, at a glance, that the connector is
// running and which printers are reachable. On first launch — when there's no
// config yet — it pairs from the menubar (paste a token, auto-discover, adopt).
// GUI dependencies (systray) live only in this binary; the headless `connector`
// binary stays dependency-free.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"

	"printer-connector/internal/agent"
	"printer-connector/internal/config"
	"printer-connector/internal/discovery"
	"printer-connector/internal/setup"
)

var version = "0.1.0"

// maxPrinterRows caps the printer rows pre-allocated in the menu. systray builds
// its menu once, so we add a hidden pool up front and show/populate it as
// printers are adopted. Sixteen is well beyond any realistic home/shop fleet.
const maxPrinterRows = 16

func main() {
	var (
		cfgPath  string
		check    bool
		logLevel string
	)
	flag.StringVar(&cfgPath, "config", "", "Path to connector config JSON (default: per-user app data)")
	flag.BoolVar(&check, "check", false, "Print status as text and exit (no GUI; for testing)")
	flag.StringVar(&logLevel, "log-level", "info", "Log level: debug|info|warn|error")
	flag.Parse()

	if cfgPath == "" {
		cfgPath = defaultConfigPath()
	}

	// A missing/invalid config means "not set up yet" — not a fatal error, so the
	// app can pair from the menubar on first launch.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = nil
	}

	if check {
		if cfg == nil {
			fmt.Printf("Connector: not set up (no config at %s)\n", cfgPath)
			return
		}
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

// defaultConfigPath returns the per-user config location. os.UserConfigDir maps
// to ~/Library/Application Support on macOS, ~/.config on Linux, %AppData% on
// Windows — so the .app finds its config without a --config flag.
func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "Spoolr", "connector.json")
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
	cfgPath string
	logger  *slog.Logger

	mu      sync.Mutex
	cfg     *config.Config // nil until paired
	running bool           // agent + refresh loop started

	cancel       context.CancelFunc
	statusItem   *systray.MenuItem
	printerItems []*systray.MenuItem
	setupItem    *systray.MenuItem
	openItem     *systray.MenuItem
	quitItem     *systray.MenuItem
}

func (m *menubar) onReady() {
	systray.SetTitle("Spoolr")
	systray.SetTooltip("Spoolr Connect")

	m.statusItem = systray.AddMenuItem("…", "Connector status")
	m.statusItem.Disable()
	systray.AddSeparator()

	for i := 0; i < maxPrinterRows; i++ {
		it := systray.AddMenuItem("", "")
		it.Disable()
		it.Hide()
		m.printerItems = append(m.printerItems, it)
	}

	systray.AddSeparator()
	m.setupItem = systray.AddMenuItem("Set up Spoolr Connect…", "Pair this computer with your Spoolr account")
	m.openItem = systray.AddMenuItem("Open Spoolr", "Open the web app")
	m.quitItem = systray.AddMenuItem("Quit", "Stop the connector and quit")

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-m.setupItem.ClickedCh:
				go m.handleSetup(ctx)
			case <-m.openItem.ClickedCh:
				openURL(m.cloudURL())
			case <-m.quitItem.ClickedCh:
				cancel()
				systray.Quit()
				return
			}
		}
	}()

	if m.paired() {
		m.setupItem.Hide()
		m.start(ctx)
	} else {
		m.renderUnpaired()
	}
}

func (m *menubar) onExit() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *menubar) paired() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg != nil
}

func (m *menubar) cloudURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg != nil && m.cfg.CloudURL != "" {
		return m.cfg.CloudURL
	}
	return config.DefaultCloudURL
}

func (m *menubar) renderUnpaired() {
	m.statusItem.SetTitle("Not set up — choose “Set up Spoolr Connect…”")
	systray.SetTooltip("Spoolr Connect — not set up")
	m.setupItem.Show()
	for _, it := range m.printerItems {
		it.Hide()
	}
}

// handleSetup runs the in-app pairing flow: prompt for a token, discover
// printers, pair, then start the agent. Runs on its own goroutine so the
// network scan never blocks the menu's click loop.
func (m *menubar) handleSetup(ctx context.Context) {
	token, ok := promptToken()
	if !ok || token == "" {
		return
	}

	m.statusItem.SetTitle("Scanning your network…")
	sctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()

	_, final, err := setup.Run(sctx, setup.Options{
		Token:      token,
		ConfigPath: m.cfgPath,
		Version:    version,
		Logger:     m.logger,
		Progress:   func(s string) { m.statusItem.SetTitle(s) },
	})
	if err != nil {
		m.statusItem.SetTitle("Setup failed — try “Set up Spoolr Connect…” again")
		errorDialog("Spoolr Connect couldn't finish setup: " + err.Error())
		m.renderUnpaired()
		return
	}

	m.mu.Lock()
	m.cfg = final
	m.mu.Unlock()

	m.setupItem.Hide()
	m.start(ctx)
}

// start launches the agent and the status refresh loop exactly once.
func (m *menubar) start(ctx context.Context) {
	m.mu.Lock()
	if m.running || m.cfg == nil {
		m.mu.Unlock()
		return
	}
	m.running = true
	cfg := m.cfg
	m.mu.Unlock()

	// Run the connector agent so this app *is* the connector.
	go func() {
		a := agent.New(agent.Options{ConfigPath: m.cfgPath, Config: cfg, Logger: m.logger, Version: version})
		if err := a.Run(ctx); err != nil && ctx.Err() == nil {
			m.logger.Warn("agent stopped", "error", err)
		}
	}()

	go m.refreshLoop(ctx)
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
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	if cfg == nil {
		return
	}

	st := computeStatus(ctx, cfg)
	for i, it := range m.printerItems {
		if i < len(st.Printers) {
			p := st.Printers[i]
			it.SetTitle(fmt.Sprintf("%s %s — %s", dot(p.Online), p.Name, label(p.Online)))
			it.Show()
		} else {
			it.Hide()
		}
	}
	m.statusItem.SetTitle(fmt.Sprintf("Connector running · %d/%d online", st.OnlineCount(), len(st.Printers)))
	systray.SetTooltip(fmt.Sprintf("Spoolr — %d/%d printers online", st.OnlineCount(), len(st.Printers)))
}

// --- native macOS dialogs (osascript) ---

// promptToken asks for a pairing token via a native dialog. Returns ("", false)
// if the user cancels, there's no GUI session, or this isn't macOS.
func promptToken() (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	const script = `display dialog "Paste your Spoolr pairing token (Spoolr app → Add printer):" ` +
		`with title "Spoolr Connect" default answer "" ` +
		`buttons {"Cancel", "Pair"} default button "Pair"`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "", false // cancelled (non-zero exit) or no GUI
	}
	// osascript prints: button returned:Pair, text returned:<token>
	s := strings.TrimSpace(string(out))
	const marker = "text returned:"
	i := strings.Index(s, marker)
	if i < 0 {
		return "", false
	}
	token := strings.TrimSpace(s[i+len(marker):])
	return token, token != ""
}

func errorDialog(msg string) {
	if runtime.GOOS != "darwin" {
		return
	}
	// AppleScript string literals can't carry newlines or double quotes.
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, `"`, "'")
	script := `display dialog "` + msg + `" with title "Spoolr Connect" buttons {"OK"} default button "OK" with icon caution`
	_ = exec.Command("osascript", "-e", script).Run()
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
