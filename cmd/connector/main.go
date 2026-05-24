package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	"printer-connector/internal/agent"
	"printer-connector/internal/config"
	"printer-connector/internal/discovery"
)

var version = "0.1.0"

func main() {
	// Subcommands: `discover` (scan the LAN) and `setup` (discover → pair →
	// adopt all). With no subcommand we keep the original `--config` agent
	// behavior so existing installs and scripts keep working unchanged.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "discover":
			runDiscover(os.Args[2:])
			return
		case "setup":
			runSetup(os.Args[2:])
			return
		}
	}
	runAgent(os.Args[1:])
}

func newLogger(level slog.Level) *slog.Logger {
	l := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(l)
	return l
}

// runAgent is the original daemon behavior: load a config and run the agent.
func runAgent(args []string) {
	fs := flag.NewFlagSet("printer-connector", flag.ExitOnError)
	var (
		cfgPath     string
		logLevel    string
		once        bool
		showVersion bool
	)
	fs.StringVar(&cfgPath, "config", "", "Path to config JSON (required)")
	fs.StringVar(&logLevel, "log-level", "info", "Log level: debug|info|warn|error")
	fs.BoolVar(&once, "once", false, "Run one iteration of each loop and exit (debug)")
	fs.BoolVar(&showVersion, "version", false, "Show version and exit")
	_ = fs.Parse(args)

	if showVersion {
		fmt.Printf("printer-connector version %s\n", version)
		os.Exit(0)
	}

	if cfgPath == "" {
		fmt.Fprintln(os.Stderr, "error: --config is required (or use the `discover` / `setup` subcommands)")
		os.Exit(2)
	}

	level := slog.LevelInfo
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		fmt.Fprintln(os.Stderr, "error: invalid --log-level (debug|info|warn|error)")
		os.Exit(2)
	}

	logger := newLogger(level)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("shutdown signal received")
		cancel()
	}()

	a := agent.New(agent.Options{
		ConfigPath: cfgPath,
		Config:     cfg,
		Logger:     logger,
		Version:    version,
		Once:       once,
	})

	if err := a.Run(ctx); err != nil {
		logger.Error("agent exited with error", "error", err)
		os.Exit(1)
	}

	logger.Info("agent exited cleanly")
}

// runDiscover scans the local network for Moonraker printers and prints them.
func runDiscover(args []string) {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	var (
		asJSON    bool
		port      int
		timeoutMs int
	)
	fs.BoolVar(&asJSON, "json", false, "Output JSON instead of a table")
	fs.IntVar(&port, "port", discovery.DefaultPort, "Moonraker port to probe")
	fs.IntVar(&timeoutMs, "timeout-ms", 600, "Per-host TCP connect timeout (ms)")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	printers, err := discovery.Scan(ctx, discovery.Options{
		Port:        port,
		DialTimeout: time.Duration(timeoutMs) * time.Millisecond,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "discovery error:", err)
		os.Exit(1)
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(printers)
		return
	}

	if len(printers) == 0 {
		fmt.Println("No printers found on the local network.")
		return
	}
	fmt.Printf("Found %d printer(s):\n\n", len(printers))
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "IP\tHOSTNAME\tSTATE\tURL")
	for _, p := range printers {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.IP, p.Hostname, p.State, p.BaseURL)
	}
	_ = w.Flush()
}

// runSetup is the one-command onboard: discover printers, write a config, pair
// with the cloud, and adopt every printer found — no hand-editing required.
func runSetup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	var (
		token    string
		cloudURL string
		cfgPath  string
		site     string
		port     int
	)
	fs.StringVar(&token, "token", "", "Pairing token from the Spoolr app (required)")
	fs.StringVar(&cloudURL, "cloud-url", "", "Cloud URL (default $CLOUD_URL or https://www.spoolr.io)")
	fs.StringVar(&cfgPath, "config", "./spoolr-connector.json", "Where to write the connector config")
	fs.StringVar(&site, "site", "", "Site name (optional)")
	fs.IntVar(&port, "port", discovery.DefaultPort, "Moonraker port to probe")
	_ = fs.Parse(args)

	logger := newLogger(slog.LevelInfo)

	if token == "" {
		fmt.Fprintln(os.Stderr, "error: --token is required (get it from the Spoolr app → Add printer)")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	fmt.Println("Scanning your network for printers…")
	found, err := discovery.Scan(ctx, discovery.Options{Port: port})
	if err != nil {
		logger.Error("discovery failed", "error", err)
		os.Exit(1)
	}
	if len(found) == 0 {
		fmt.Println("No printers found. Make sure they're powered on and on this network (try --port if Moonraker runs elsewhere).")
		os.Exit(1)
	}

	fmt.Printf("Found %d printer(s):\n", len(found))
	printers := make([]config.Printer, 0, len(found))
	for _, p := range found {
		name := p.Hostname
		if name == "" {
			name = p.IP
		}
		fmt.Printf("  • %s (%s) — %s\n", name, p.IP, p.State)
		printers = append(printers, config.Printer{
			Type:    config.TypeMoonraker,
			Name:    name,
			BaseURL: p.BaseURL,
			UIPort:  80,
		})
	}

	cloud := cloudURL
	if cloud == "" {
		cloud = os.Getenv("CLOUD_URL")
	}
	if cloud == "" {
		cloud = config.DefaultCloudURL
	}

	cfg := &config.Config{
		CloudURL:     cloud,
		PairingToken: token,
		SiteName:     site,
		StateDir:     filepath.Join(filepath.Dir(cfgPath), "state"),
		Printers:     printers,
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(1)
	}
	if err := config.SaveAtomic(cfgPath, cfg); err != nil {
		logger.Error("could not write config", "error", err)
		os.Exit(1)
	}

	fmt.Println("\nPairing with Spoolr and adopting printers…")
	a := agent.New(agent.Options{
		ConfigPath: cfgPath,
		Config:     cfg,
		Logger:     logger,
		Version:    version,
		Once:       true,
	})
	if err := a.Run(ctx); err != nil {
		logger.Error("pairing failed", "error", err)
		os.Exit(1)
	}

	final, err := config.Load(cfgPath)
	if err != nil {
		logger.Error("paired but could not re-read config", "error", err)
		os.Exit(1)
	}
	fmt.Printf("\n✅ Done — adopted %d printer(s):\n", len(final.Printers))
	for _, p := range final.Printers {
		fmt.Printf("  • %s → printer #%d\n", p.Name, p.PrinterID)
	}
	fmt.Printf("\nConfig written to %s\nKeep them online with:\n  printer-connector --config %s\n", cfgPath, cfgPath)
}
