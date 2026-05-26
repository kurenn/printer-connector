//go:build windows

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"

	"printer-connector/internal/agent"
	"printer-connector/internal/config"
)

const (
	serviceName        = "SpoolrConnect"
	serviceDisplayName = "Spoolr Connect"
	serviceDescription = "Spoolr Connect — 3D printer fleet connector agent"
)

// connectorService implements svc.Handler. It starts the agent loop in a
// goroutine and forwards Windows SCM control requests (Stop, Shutdown) to it
// via context cancellation.
type connectorService struct {
	configPath string
}

// Execute is called by the Windows Service Control Manager. It runs the agent
// and handles Stop/Shutdown gracefully, returning once the agent has exited.
func (s *connectorService) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (ssec bool, errno uint32) {
	// Signal "start pending" while we initialise.
	status <- svc.Status{State: svc.StartPending}

	// Set up a logger that writes to the Windows Event Log so service output is
	// visible in Event Viewer without a console window.
	elog, err := eventlog.Open(serviceName)
	if err != nil {
		// Fall back to stderr-based logger if Event Log is unavailable.
		elog = nil
	}

	logInfo := func(msg string) {
		if elog != nil {
			_ = elog.Info(1, msg)
		}
	}
	logError := func(msg string) {
		if elog != nil {
			_ = elog.Error(1, msg)
		}
	}

	// Load and validate config.
	cfg, err := config.Load(s.configPath)
	if err != nil {
		logError("failed to load config: " + err.Error())
		return false, 1
	}
	if err := cfg.Validate(); err != nil {
		logError("invalid config: " + err.Error())
		return false, 1
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run the agent in a goroutine so we can keep reading the SCM channel.
	done := make(chan error, 1)
	go func() {
		a := agent.New(agent.Options{
			ConfigPath: s.configPath,
			Config:     cfg,
			Logger:     logger,
			Version:    version,
		})
		done <- a.Run(ctx)
	}()

	// Tell the SCM we are running and which controls we accept.
	status <- svc.Status{
		State:   svc.Running,
		Accepts: svc.AcceptStop | svc.AcceptShutdown,
	}
	logInfo("Spoolr Connect service started")

	// Main SCM event loop.
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				logInfo("Spoolr Connect service stopping")
				status <- svc.Status{State: svc.StopPending}
				cancel()
				// Give the agent up to 30 s to clean up.
				select {
				case <-done:
				case <-time.After(30 * time.Second):
				}
				return false, 0
			default:
				logError(fmt.Sprintf("unexpected control request: %d", c.Cmd))
			}
		case err := <-done:
			if err != nil {
				logError("agent exited with error: " + err.Error())
			} else {
				logInfo("agent exited cleanly")
			}
			return false, 0
		}
	}
}

// runService hands control to the Windows SCM. This is called by the
// "run-service" subcommand when the SCM starts the process.
func runService(configPath string) error {
	return svc.Run(serviceName, &connectorService{configPath: configPath})
}

// installService registers and starts the Spoolr Connect Windows Service.
// exePath must be the absolute path to this executable.
// configPath will be passed as --config to the "run-service" subcommand.
// The function is idempotent: if the service already exists it opens it and
// ensures it is started.
func installService(exePath, configPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	// Attempt to open an existing service first (idempotent path).
	existing, err := m.OpenService(serviceName)
	if err == nil {
		// Service exists — just make sure it's running.
		defer existing.Close()
		status, queryErr := existing.Query()
		if queryErr == nil && status.State != svc.Stopped && status.State != svc.StopPending {
			return nil // already running
		}
		return existing.Start()
	}

	// Service does not exist — create it.
	binPath := fmt.Sprintf(`"%s" run-service --config "%s"`, exePath, configPath)
	s, err := m.CreateService(serviceName, binPath, mgr.Config{
		DisplayName:  serviceDisplayName,
		Description:  serviceDescription,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	})
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	// Register an event source so the service can write to Event Viewer.
	_ = eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)

	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

// removeService stops and deletes the Spoolr Connect Windows Service.
func removeService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		// Service not found — nothing to remove.
		return nil
	}
	defer s.Close()

	// Stop first (best-effort).
	_, _ = s.Control(svc.Stop)

	// Wait briefly for the service to reach Stopped state.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st, qErr := s.Query()
		if qErr != nil || st.State == svc.Stopped {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}

	_ = eventlog.Remove(serviceName)
	return nil
}

// windowsConfigPath returns the default config path for a Windows service:
// %ProgramData%\SpoolrConnect\connector.json. The service runs as LocalSystem,
// which has full access to ProgramData.
func windowsConfigPath() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, "SpoolrConnect", "connector.json")
}
