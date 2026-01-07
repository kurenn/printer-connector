package agent

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"runtime"
	"time"

	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
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

	cloud *cloud.Client
	moons map[int]*moonraker.Client

	startedAt time.Time
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

	moons := map[int]*moonraker.Client{}
	for _, p := range opts.Config.Moonraker {
		moons[p.PrinterID] = moonraker.New(p.BaseURL, p.UIPort)
	}

	return &Agent{
		cfgPath:   opts.ConfigPath,
		cfg:       opts.Config,
		log:       opts.Logger,
		version:   opts.Version,
		once:      opts.Once,
		cloud:     cl,
		moons:     moons,
		startedAt: time.Now(),
	}
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
		"printers", len(a.cfg.Moonraker),
	)

	if a.once {
		_ = a.sendHeartbeat(ctx)
		_ = a.pollAndExecuteCommands(ctx)
		_ = a.collectAndPushSnapshots(ctx)
		_ = a.processWebcamRequests(ctx)
		return nil
	}

	errCh := make(chan error, 4)
	go func() { errCh <- a.heartbeatLoop(ctx) }()
	go func() { errCh <- a.commandsLoop(ctx) }()
	go func() { errCh <- a.snapshotsLoop(ctx) }()
	go func() { errCh <- a.webcamLoop(ctx) }()

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

func (a *Agent) pair(ctx context.Context) error {
	hostname, _ := os.Hostname()

	var uiPort int
	if len(a.cfg.Moonraker) > 0 {
		uiPort = a.cfg.Moonraker[0].UIPort
	}

	// Build printers array from moonraker config
	printers := make([]cloud.PrinterInfo, 0, len(a.cfg.Moonraker))
	for _, m := range a.cfg.Moonraker {
		printers = append(printers, cloud.PrinterInfo{
			Name:   m.Name,
			UIPort: m.UIPort,
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
			if i < len(a.cfg.Moonraker) {
				a.cfg.Moonraker[i].PrinterID = printer.ID
				a.log.Info("mapped printer",
					"moonraker_name", a.cfg.Moonraker[i].Name,
					"printer_id", printer.ID,
					"rails_name", printer.Name)
			}
		}
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
			time.Sleep(bo.Next())
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
			time.Sleep(bo.Next())
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
			time.Sleep(bo.Next())
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
			time.Sleep(bo.Next())
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
	moon, ok := a.moons[req.PrinterID]
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
