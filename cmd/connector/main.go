package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"printer-connector/internal/agent"
	"printer-connector/internal/config"
	"printer-connector/internal/discovery"
)

var version = "0.1.0"

// runDiscover performs a LAN sweep for Moonraker printers and prints the result
// as JSON on stdout. Standalone (no --config); used by the macOS menubar app's
// "Scan network" flow.
func runDiscover() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res := discovery.Scan(ctx)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(res)
}

func main() {
	// Subcommands are intercepted before flag parsing / the --config check.
	if len(os.Args) > 1 && os.Args[1] == "discover" {
		runDiscover()
		return
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
