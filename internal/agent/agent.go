package agent

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"

	"printer-connector/internal/bambu"
	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
	"printer-connector/internal/driver"
	"printer-connector/internal/moonraker"
	"printer-connector/internal/util"
)

type Options struct {
	ConfigPath string
	Config     *config.Config
	Logger     *slog.Logger
	Version    string
	Once       bool
}

type Agent struct {
	cfgPath string
	cfg     *config.Config
	log     *slog.Logger
	version string
	once    bool

	cloud   *cloud.Client
	drivers map[int]driver.Driver

	startedAt time.Time

	// lastSnapshotPushUnix is the UnixNano of the last snapshot batch that was
	// successfully pushed with at least one printer. The watchdog reads it to
	// detect a telemetry stall; collectAndPushSnapshots writes it on success.
	lastSnapshotPushUnix atomic.Int64

	// restartDelay is the pause before superviseLoop restarts a faulted loop.
	// Set from New(); a zero value (used in tests) restarts immediately.
	restartDelay time.Duration
}

func New(opts Options) *Agent {
	userAgent := "printer-connector/" + opts.Version

	cl := cloud.New(cloud.Options{
		BaseURL:         opts.Config.CloudURL,
		ConnectorID:     opts.Config.ConnectorID,
		ConnectorSecret: opts.Config.ConnectorSecret,
		Logger:          opts.Logger,
		UserAgent:       userAgent,
	})

	return &Agent{
		cfgPath:      opts.ConfigPath,
		cfg:          opts.Config,
		log:          opts.Logger,
		version:      opts.Version,
		once:         opts.Once,
		cloud:        cl,
		drivers:      buildDrivers(opts.Config.Printers),
		startedAt:    time.Now(),
		restartDelay: loopRestartDelay,
	}
}

// buildDrivers creates one driver per configured printer, keyed by printer ID,
// dispatching on the printer's protocol type. It must be rebuilt whenever the
// printer IDs change (e.g. after pairing populates them), otherwise lookups by
// the real ID miss. Config validation guarantees the type is one handled here.
func buildDrivers(printers []config.Printer) map[int]driver.Driver {
	drivers := make(map[int]driver.Driver, len(printers))
	for _, p := range printers {
		switch p.Type {
		case config.TypeBambu:
			drivers[p.PrinterID] = bambu.New(p.Host, p.Serial, p.AccessCode)
		default: // moonraker (the empty type defaults to moonraker in Load)
			drivers[p.PrinterID] = moonraker.New(p.BaseURL, p.UIPort)
		}
	}
	return drivers
}

func (a *Agent) Run(ctx context.Context) error {
	if a.cfg.PairingToken != "" {
		if err := a.pair(ctx); err != nil {
			return err
		}
	}

	a.log.Info("connector running",
		"connector_id", a.cfg.ConnectorID,
		"cloud_url", a.cfg.CloudURL,
		"printers", len(a.cfg.Printers),
	)

	if a.once {
		_ = a.sendHeartbeat(ctx)
		_ = a.pollAndExecuteCommands(ctx)
		_ = a.collectAndPushSnapshots(ctx)
		_ = a.processWebcamRequests(ctx)
		// Webcam streaming is a long-lived relay, not a one-shot poll, so it is
		// intentionally skipped in --once mode.
		return nil
	}

	// Seed the stall clock so the watchdog measures from startup: if no batch
	// is pushed within the threshold after boot, that is itself a stall.
	a.lastSnapshotPushUnix.Store(time.Now().UnixNano())

	// Each loop is supervised: a panic or unexpected return is recovered, logged
	// loudly, and the loop restarted, so one faulting loop can neither vanish
	// silently nor crash the sibling loops (heartbeat, commands, webcam).
	errCh := make(chan error, 6)
	go func() { errCh <- a.superviseLoop(ctx, "heartbeat", a.heartbeatLoop) }()
	go func() { errCh <- a.superviseLoop(ctx, "commands", a.commandsLoop) }()
	go func() { errCh <- a.superviseLoop(ctx, "snapshots", a.snapshotsLoop) }()
	go func() { errCh <- a.superviseLoop(ctx, "webcam", a.webcamLoop) }()
	go func() { errCh <- a.superviseLoop(ctx, "webcam_stream", a.webcamStreamLoop) }()
	go func() { errCh <- a.superviseLoop(ctx, "watchdog", a.watchdogLoop) }()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}

// watchdogLoop watches the snapshot stall clock and logs loudly when telemetry
// goes quiet, so a connector that is heartbeating but pushing no snapshots —
// the failure that makes printers read "offline" while the connector looks
// online — is observable instead of silent. It never pushes telemetry itself.
func (a *Agent) watchdogLoop(ctx context.Context) error {
	interval := time.Duration(a.cfg.PushSnapshotsSeconds) * time.Second
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	// Allow a few missed pushes before alarming, but never less than 90s — the
	// same window after which the dashboard flips a printer to offline.
	threshold := 3 * time.Duration(a.cfg.PushSnapshotsSeconds) * time.Second
	if threshold < 90*time.Second {
		threshold = 90 * time.Second
	}

	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}

		// Nothing to stall on when no printers are configured.
		if len(a.cfg.Printers) == 0 {
			continue
		}

		last := time.Unix(0, a.lastSnapshotPushUnix.Load())
		if snapshotStalled(last, time.Now(), threshold) {
			a.log.Error("snapshot telemetry stalled — printers will read offline in the dashboard",
				"stalled_for", time.Since(last).Round(time.Second).String(),
				"last_push", last.UTC().Format(time.RFC3339),
				"threshold", threshold.String(),
				"hint", "connector is alive (heartbeating) but cannot reach any printer; check Moonraker/printer power and LAN",
			)
		}
	}
}

// hostPortFromBaseURL extracts host + port from a Moonraker base URL such as
// "http://192.168.1.70:7125". Port defaults to 7125 when absent.
func hostPortFromBaseURL(baseURL string) (string, int) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return "", 0
	}
	port := 7125
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	return u.Hostname(), port
}

func (a *Agent) pair(ctx context.Context) error {
	hostname, _ := os.Hostname()

	var uiPort int
	if len(a.cfg.Printers) > 0 {
		uiPort = a.cfg.Printers[0].UIPort
	}

	// Build printers array from config, including each printer's real LAN
	// host:port so Rails gives multiple printers distinct identities (otherwise
	// they all fall back to the connector IP and collide on host+port).
	printers := make([]cloud.PrinterInfo, 0, len(a.cfg.Printers))
	for _, m := range a.cfg.Printers {
		host, mport := hostPortFromBaseURL(m.BaseURL)
		if m.Host != "" {
			host = m.Host // Bambu printers carry their own host directly
		}
		printers = append(printers, cloud.PrinterInfo{
			Name:          m.Name,
			Type:          m.Type, // "moonraker" (default) or "bambu" → cloud sets printer_type
			Host:          host,
			MoonrakerPort: mport, // 0 for Bambu (no Moonraker port)
			UIPort:        m.UIPort,
		})
	}

	req := cloud.RegisterRequest{
		PairingToken: a.cfg.PairingToken,
		SiteName:     a.cfg.SiteName,
		Device: cloud.DeviceInfo{
			Hostname: hostname,
			Arch:     runtime.GOARCH,
			OS:       runtime.GOOS,
			Version:  a.version,
			IP:       getLocalIP(),
			UIPort:   uiPort,
		},
		Printers: printers,
	}

	a.log.Info("pairing connector (register)")
	resp, err := a.cloud.Register(ctx, req)
	if err != nil {
		return err
	}

	a.cfg.ConnectorID = string(resp.Connector.ID)
	a.cfg.ConnectorSecret = resp.Credentials.Secret
	a.cfg.PairingToken = ""

	if resp.Polling.CommandsSeconds > 0 {
		a.cfg.PollCommandsSeconds = resp.Polling.CommandsSeconds
	}
	if resp.Polling.SnapshotsSeconds > 0 {
		a.cfg.PushSnapshotsSeconds = resp.Polling.SnapshotsSeconds
	}

	// Auto-populate printer_ids from Rails response
	if len(resp.Printers) > 0 {
		for i, printer := range resp.Printers {
			// Match by index (first printer in response -> first moonraker entry)
			if i < len(a.cfg.Printers) {
				a.cfg.Printers[i].PrinterID = printer.ID
				a.log.Info("mapped printer",
					"moonraker_name", a.cfg.Printers[i].Name,
					"printer_id", printer.ID,
					"rails_name", printer.Name)
			}
		}

		// Rebuild the Moonraker client map: it was keyed by the pre-pairing
		// printer IDs (all 0), so without this the post-pairing loops would
		// look up the real IDs and find nothing.
		a.drivers = buildDrivers(a.cfg.Printers)
	}

	if err := config.SaveAtomic(a.cfgPath, a.cfg); err != nil {
		return err
	}

	a.cloud.SetCredentials(a.cfg.ConnectorID, a.cfg.ConnectorSecret)
	a.log.Info("paired successfully", "connector_id", a.cfg.ConnectorID)
	return nil
}

func (a *Agent) heartbeatLoop(ctx context.Context) error {
	tick := time.NewTicker(time.Duration(a.cfg.HeartbeatSeconds) * time.Second)
	defer tick.Stop()

	bo := util.NewBackoff(1*time.Second, 60*time.Second)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := a.sendHeartbeat(ctx); err != nil {
			a.log.Warn("heartbeat failed", "error", err)
			if werr := util.Wait(ctx, bo.Next()); werr != nil {
				return werr
			}
		} else {
			bo.Reset()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (a *Agent) commandsLoop(ctx context.Context) error {
	tick := time.NewTicker(time.Duration(a.cfg.PollCommandsSeconds) * time.Second)
	defer tick.Stop()

	bo := util.NewBackoff(1*time.Second, 60*time.Second)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := a.pollAndExecuteCommands(ctx); err != nil {
			a.log.Warn("commands poll failed", "error", err)
			if werr := util.Wait(ctx, bo.Next()); werr != nil {
				return werr
			}
		} else {
			bo.Reset()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (a *Agent) snapshotsLoop(ctx context.Context) error {
	tick := time.NewTicker(time.Duration(a.cfg.PushSnapshotsSeconds) * time.Second)
	defer tick.Stop()

	bo := util.NewBackoff(1*time.Second, 60*time.Second)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := a.collectAndPushSnapshots(ctx); err != nil {
			a.log.Warn("snapshots push failed", "error", err)
			if werr := util.Wait(ctx, bo.Next()); werr != nil {
				return werr
			}
		} else {
			bo.Reset()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (a *Agent) webcamLoop(ctx context.Context) error {
	// Poll webcam requests every 2 seconds (more frequent than snapshots for responsiveness)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	bo := util.NewBackoff(1*time.Second, 60*time.Second)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := a.processWebcamRequests(ctx); err != nil {
			a.log.Warn("webcam requests processing failed", "error", err)
			if werr := util.Wait(ctx, bo.Next()); werr != nil {
				return werr
			}
		} else {
			bo.Reset()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (a *Agent) processWebcamRequests(ctx context.Context) error {
	// Fetch pending webcam requests
	requests, err := a.cloud.GetWebcamRequests(ctx, 10)
	if err != nil {
		return err
	}

	if len(requests) == 0 {
		return nil
	}

	a.log.Debug("processing webcam requests", "count", len(requests))

	// Process each request
	for _, req := range requests {
		if err := a.handleWebcamRequest(ctx, req); err != nil {
			a.log.Error("failed to process webcam request",
				"request_id", req.ID.String(),
				"printer_id", req.PrinterID,
				"error", err,
			)
			// Continue processing other requests even if one fails
		}
	}

	return nil
}

func (a *Agent) handleWebcamRequest(ctx context.Context, req cloud.WebcamRequest) error {
	// Find the moonraker client for this printer
	moon, ok := a.drivers[req.PrinterID]
	if !ok {
		return a.cloud.UploadWebcamSnapshot(ctx, req.ID, req.PrinterID, nil, "application/json")
	}

	// Fetch snapshot from Moonraker
	imageData, contentType, err := moon.GetWebcamSnapshot(ctx)
	if err != nil {
		a.log.Warn("failed to fetch webcam snapshot from moonraker",
			"printer_id", req.PrinterID,
			"error", err,
		)
		// Best-effort: mark the request failed so Rails stops returning it in the
		// pending list and the webcam loop stops retrying it every 2 seconds.
		if markErr := a.cloud.MarkWebcamRequestFailed(ctx, req.ID, err.Error()); markErr != nil {
			a.log.Warn("failed to mark webcam request as failed",
				"request_id", req.ID.String(),
				"error", markErr,
			)
		}
		return err
	}

	// Upload to Rails
	if err := a.cloud.UploadWebcamSnapshot(ctx, req.ID, req.PrinterID, imageData, contentType); err != nil {
		return err
	}

	a.log.Info("webcam snapshot uploaded",
		"request_id", req.ID.String(),
		"printer_id", req.PrinterID,
		"size_bytes", len(imageData),
	)

	return nil
}

// getLocalIP returns the non-loopback local IP address of the machine
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
