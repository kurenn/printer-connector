// Package setup is the one-command onboarding flow shared by the headless
// `connector setup` CLI and the macOS menubar app: discover printers on the
// LAN, write a config, and pair with the cloud (adopting every printer found).
// Keeping it here means both front-ends pair identically.
package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"printer-connector/internal/agent"
	"printer-connector/internal/config"
	"printer-connector/internal/discovery"
)

// Options configures a setup run. Token and ConfigPath are required; the rest
// fall back to sensible defaults.
type Options struct {
	Token      string
	CloudURL   string // defaults to $CLOUD_URL then config.DefaultCloudURL
	ConfigPath string
	Site       string
	Port       int // Moonraker probe port; 0 -> discovery.DefaultPort
	Version    string
	Logger     *slog.Logger
	// Progress, if set, receives human-readable status lines ("Scanning…",
	// "Found 3 printer(s) — pairing…"). Lets the CLI print and the menubar
	// update its status item from the same flow.
	Progress func(string)
}

// Run discovers printers, writes the config, and pairs with the cloud. It
// returns the printers found on the LAN and the final (post-pairing) config,
// which carries the connector credentials and per-printer IDs assigned by the
// cloud. All network and file work is bounded by ctx.
func Run(ctx context.Context, opts Options) (found []discovery.Printer, final *config.Config, err error) {
	if opts.Token == "" {
		return nil, nil, errors.New("pairing token is required")
	}
	if opts.ConfigPath == "" {
		return nil, nil, errors.New("config path is required")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	progress := opts.Progress
	if progress == nil {
		progress = func(string) {}
	}

	port := opts.Port
	if port == 0 {
		port = discovery.DefaultPort
	}

	progress("Scanning your network for printers…")
	found, err = discovery.Scan(ctx, discovery.Options{Port: port})
	if err != nil {
		return nil, nil, fmt.Errorf("discovery failed: %w", err)
	}
	if len(found) == 0 {
		return found, nil, errors.New("no printers found — make sure they're powered on and on this network")
	}
	progress(fmt.Sprintf("Found %d printer(s) — pairing with Spoolr…", len(found)))

	printers := make([]config.Printer, 0, len(found))
	for _, p := range found {
		name := p.Hostname
		if name == "" {
			name = p.IP
		}
		printers = append(printers, config.Printer{
			Type:    config.TypeMoonraker,
			Name:    name,
			BaseURL: p.BaseURL,
			UIPort:  80,
		})
	}

	cloud := opts.CloudURL
	if cloud == "" {
		cloud = os.Getenv("CLOUD_URL")
	}
	if cloud == "" {
		cloud = config.DefaultCloudURL
	}

	cfg := &config.Config{
		CloudURL:     cloud,
		PairingToken: opts.Token,
		SiteName:     opts.Site,
		StateDir:     filepath.Join(filepath.Dir(opts.ConfigPath), "state"),
		Printers:     printers,
	}
	if err = cfg.Validate(); err != nil {
		return found, nil, fmt.Errorf("invalid config: %w", err)
	}
	if err = config.SaveAtomic(opts.ConfigPath, cfg); err != nil {
		return found, nil, fmt.Errorf("could not write config: %w", err)
	}

	// One agent iteration registers the connector and adopts the printers,
	// then rewrites the config with the cloud-assigned credentials and IDs.
	a := agent.New(agent.Options{
		ConfigPath: opts.ConfigPath,
		Config:     cfg,
		Logger:     logger,
		Version:    opts.Version,
		Once:       true,
	})
	if err = a.Run(ctx); err != nil {
		return found, nil, fmt.Errorf("pairing failed: %w", err)
	}

	final, err = config.Load(opts.ConfigPath)
	if err != nil {
		return found, nil, fmt.Errorf("paired but could not re-read config: %w", err)
	}
	return found, final, nil
}
