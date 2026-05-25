package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"printer-connector/internal/agent"
	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
	"printer-connector/internal/discovery"
)

var version = "0.1.0"

// bambuFlag collects repeated --bambu specs ("host,serial,accesscode,name").
type bambuFlag []string

func (b *bambuFlag) String() string { return strings.Join(*b, " ") }
func (b *bambuFlag) Set(v string) error {
	*b = append(*b, v)
	return nil
}

// runDiscover sweeps the LAN for Moonraker printers AND listens for Bambu SSDP
// beacons, printing both as JSON. Standalone (no --config); powers the menubar
// app's "Scan network" flow.
func runDiscover() {
	bambuOnly := false
	for _, a := range os.Args[2:] {
		if a == "--bambu-only" {
			bambuOnly = true
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var (
		moon  discovery.Result
		bambu []discovery.BambuPrinter
		wg    sync.WaitGroup
	)
	if bambuOnly {
		// SSDP-only: skip the Moonraker /24 sweep (used as a fast opt-in probe
		// so the common Klipper flow isn't slowed by a Bambu scan it doesn't need).
		bambu = discovery.ScanBambu(ctx, 4*time.Second)
	} else {
		wg.Add(2)
		go func() { defer wg.Done(); moon = discovery.Scan(ctx) }()
		go func() { defer wg.Done(); bambu = discovery.ScanBambu(ctx, 4*time.Second) }()
		wg.Wait()
	}

	// Always emit a non-nil printers array (the menubar app decodes it as a
	// required field; a null in --bambu-only mode would fail the decode).
	moonPrinters := moon.Printers
	if moonPrinters == nil {
		moonPrinters = []discovery.Printer{}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"hosts_total":  moon.HostsTotal,
		"hosts_probed": moon.HostsProbed,
		"subnets":      moon.Subnets,
		"printers":     moonPrinters,
		"bambu":        bambu, // need a user-entered access code before pairing
	})
}

// runRegister discovers every Moonraker printer on the LAN and registers them
// all under one pairing token (token → adds all printers → web UI updates),
// persisting credentials to connector.json. Prints the result as JSON.
//
//   connector register --token <T> [--cloud <URL>] [--site <S>] [--config <P>]
func runRegister() {
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	var token, cloudURL, site, cfgPath string
	var bambu bambuFlag
	fs.StringVar(&token, "token", "", "Pairing token (required)")
	fs.StringVar(&cloudURL, "cloud", "", "Cloud URL override")
	fs.StringVar(&site, "site", "", "Site name")
	fs.StringVar(&cfgPath, "config", defaultConfigPath(), "Config path")
	fs.Var(&bambu, "bambu", "Bambu printer 'host,serial,accesscode,name' (repeatable)")
	_ = fs.Parse(os.Args[2:])

	emitErr := func(msg string) {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"error": msg})
		os.Exit(1)
	}
	if token == "" {
		emitErr("pairing token is required (--token)")
	}

	// Resolve cloud URL + site: flag > existing config > env > default.
	existing, _ := config.Load(cfgPath)
	if cloudURL == "" && existing != nil {
		cloudURL = existing.CloudURL
	}
	if cloudURL == "" {
		cloudURL = os.Getenv("CLOUD_URL")
	}
	if cloudURL == "" {
		cloudURL = config.DefaultCloudURL
	}
	if site == "" && existing != nil {
		site = existing.SiteName
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	found := discovery.Scan(ctx)

	cfgPrinters := make([]config.Printer, 0, len(found.Printers)+len(bambu))
	regPrinters := make([]cloud.PrinterInfo, 0, len(found.Printers)+len(bambu))

	// Bambu printers come from the UI as "host,serial,accesscode,name" (the
	// access code can't be discovered — the user reads it off the printer).
	// Credentials stay in connector.json; only name/type/host go to the cloud.
	for _, spec := range bambu {
		parts := strings.SplitN(spec, ",", 4)
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		host, serial, code := parts[0], parts[1], parts[2]
		name := host
		if len(parts) == 4 && parts[3] != "" {
			name = parts[3]
		}
		cfgPrinters = append(cfgPrinters, config.Printer{
			Type:       config.TypeBambu,
			Name:       name,
			Host:       host,
			Serial:     serial,
			AccessCode: code,
		})
		regPrinters = append(regPrinters, cloud.PrinterInfo{
			Name: name,
			Type: config.TypeBambu,
			Host: host,
		})
	}

	if len(found.Printers) == 0 && len(regPrinters) == 0 {
		emitErr("no printers found on the network")
	}

	for _, p := range found.Printers {
		cfgPrinters = append(cfgPrinters, config.Printer{
			Type:    config.TypeMoonraker,
			Name:    p.Name,
			BaseURL: fmt.Sprintf("http://%s:%d", p.Host, p.Port),
			UIPort:  80,
		})
		regPrinters = append(regPrinters, cloud.PrinterInfo{
			Name:          p.Name,
			Host:          p.Host,
			MoonrakerPort: p.Port,
			UIPort:        80,
		})
	}

	hostname, _ := os.Hostname()
	client := cloud.New(cloud.Options{
		BaseURL:   cloudURL,
		Logger:    slog.Default(),
		UserAgent: "spoolr-connect/" + version,
	})
	resp, err := client.Register(ctx, cloud.RegisterRequest{
		PairingToken: token,
		SiteName:     site,
		Device: cloud.DeviceInfo{
			Hostname: hostname,
			Arch:     runtime.GOARCH,
			OS:       runtime.GOOS,
			Version:  version,
			IP:       localIP(),
			UIPort:   80,
		},
		Printers: regPrinters,
	})
	if err != nil {
		emitErr("register failed: " + err.Error())
	}

	cfg := &config.Config{
		CloudURL:        cloudURL,
		ConnectorID:     string(resp.Connector.ID),
		ConnectorSecret: resp.Credentials.Secret,
		SiteName:        site,
		Printers:        cfgPrinters,
	}
	if resp.Polling.CommandsSeconds > 0 {
		cfg.PollCommandsSeconds = resp.Polling.CommandsSeconds
	}
	if resp.Polling.SnapshotsSeconds > 0 {
		cfg.PushSnapshotsSeconds = resp.Polling.SnapshotsSeconds
	}

	// Report only what Rails actually created/adopted. Printers it skipped
	// (e.g. already owned by another workspace) must NOT be reported as linked.
	// Map the returned ids back onto our config by name.
	byName := make(map[string]int, len(resp.Printers))
	for _, rp := range resp.Printers {
		byName[rp.Name] = rp.ID
	}
	for i := range cfg.Printers {
		if id, ok := byName[cfg.Printers[i].Name]; ok {
			cfg.Printers[i].PrinterID = id
		}
	}

	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o755)
	if err := config.SaveAtomic(cfgPath, cfg); err != nil {
		emitErr("saving config failed: " + err.Error())
	}

	if len(resp.Printers) == 0 {
		emitErr(fmt.Sprintf(
			"discovered %d printer(s) but none were linked — they may already belong to another Spoolr workspace",
			len(found.Printers)))
	}

	out := make([]map[string]any, 0, len(resp.Printers))
	for _, rp := range resp.Printers {
		out = append(out, map[string]any{"id": rp.ID, "name": rp.Name})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"connector_id": string(resp.Connector.ID),
		"site":         site,
		"cloud_url":    cloudURL,
		"config_path":  cfgPath,
		"count":        len(out),
		"printers":     out,
	})
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir() // ~/Library/Application Support on macOS
	if err != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Library", "Application Support")
	}
	return filepath.Join(dir, "Spoolr", "connector.json")
}

func localIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

func main() {
	// Subcommands are intercepted before flag parsing / the --config check.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "discover":
			runDiscover()
			return
		case "register":
			runRegister()
			return
		}
	}

	var (
		cfgPath     string
		logLevel    string
		once        bool
		showVersion bool
	)
	flag.StringVar(&cfgPath, "config", "", "Path to config JSON (required)")
	flag.StringVar(&logLevel, "log-level", "info", "Log level: debug|info|warn|error")
	flag.BoolVar(&once, "once", false, "Run one iteration of each loop and exit (debug)")
	flag.BoolVar(&showVersion, "version", false, "Show version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("printer-connector version %s\n", version)
		os.Exit(0)
	}

	if cfgPath == "" {
		fmt.Fprintln(os.Stderr, "error: --config is required")
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

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

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
