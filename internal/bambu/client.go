// Package bambu is the Bambu Lab driver. It speaks the printer's LAN/developer
// protocol directly: MQTT over TLS (port 8883) for telemetry and print control,
// and implicit FTPS (port 990) for the file transfer that backs start_print.
// Both authenticate with the printer's serial number and access code.
//
// The driver holds a single long-lived MQTT session per printer, connected
// lazily on first use and kept alive with auto-reconnect. Bambu pushes state as
// partial diffs that the transport deep-merges; QueryObjects returns that merged
// raw report and NormalizeRaw turns it into canonical telemetry. The agent
// attaches the normalized view to each snapshot via NormalizeRaw, so the cloud —
// which has no Klipper-style object model to read for Bambu — consumes Bambu
// snapshots through the normalized payload.
//
// LAN control requires LAN Only Mode. A cloud-bound Bambu (the default, so the
// owner keeps Bambu Handy) accepts LAN MQTT for reading telemetry but silently
// ignores print-control commands (pause/resume/cancel/project_file) — verified
// on an A1 mini running firmware 01.08.01.00, where every control command was
// dropped at any QoS while all telemetry flowed normally. Switching the printer
// to "LAN Only Mode" opens the control channel. So Bambu monitoring works out of
// the box; Bambu *control* through the connector requires the printer in LAN
// Only Mode. Telemetry is unaffected either way.
package bambu

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"printer-connector/internal/driver"
)

// ErrNotSupported is returned by actions the Bambu LAN protocol does not expose
// through this driver (e.g. homing, history, webcam). It is distinct from a
// transient failure so callers can tell "never works" from "didn't work now".
var ErrNotSupported = errors.New("bambu driver: action not supported")

// Client is the Bambu Lab driver.Driver implementation.
type Client struct {
	host       string
	serial     string
	accessCode string

	// Seams for testing: the real implementations dial the printer, fakes are
	// injected in tests so logic is exercised without hardware.
	dial      func(ctx context.Context) (transport, error)
	ftpUpload func(host, accessCode, filename string, content []byte) error
	ftpDelete func(host, accessCode, filename string) error
	ftpList   func(host, accessCode string) ([]map[string]any, error)

	mu  sync.Mutex
	tr  transport
	seq atomic.Int64
}

var _ driver.Driver = (*Client)(nil)

// New constructs a Bambu driver for a printer reachable at host, authenticating
// with its serial number and access code. No connection is opened until the
// first telemetry query or command.
func New(host, serial, accessCode string) *Client {
	c := &Client{host: host, serial: serial, accessCode: accessCode}
	c.dial = func(ctx context.Context) (transport, error) {
		return dialMQTT(ctx, c.host, c.serial, c.accessCode)
	}
	c.ftpUpload = ftpsUpload
	c.ftpDelete = ftpsDelete
	c.ftpList = ftpsList
	return c
}

// Capabilities reports the actions this driver implements. Homing, history and
// webcam are intentionally absent — see the ErrNotSupported methods below — so
// the cloud only offers controls that work.
func (c *Client) Capabilities() []string {
	return []string{"pause", "resume", "cancel", "start_print", "upload_file", "delete_file", "sync_files"}
}

// ensure connects the MQTT session once, lazily. After a successful connect the
// session auto-reconnects on drops; a failed connect leaves tr nil so the next
// call retries from scratch (matching the snapshot loop's cadence).
func (c *Client) ensure(ctx context.Context) (transport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tr != nil {
		return c.tr, nil
	}
	tr, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	c.tr = tr
	return tr, nil
}

func (c *Client) requestTopic() string { return "device/" + c.serial + "/request" }

func (c *Client) publishPrint(ctx context.Context, payload []byte) error {
	tr, err := c.ensure(ctx)
	if err != nil {
		return err
	}
	// QoS 0, not 1. Bambu's broker never sends a PUBACK for QoS-1 publishes on
	// the request topic — verified on an A1 mini: every control command blocked
	// the full publish timeout and returned an error even when the printer
	// obeyed. QoS 0 is what the printer expects (it's what the seed publishes
	// and Bambu Studio itself use) and completes immediately, so the command's
	// success is no longer masked by a phantom timeout. Delivery is confirmed
	// out-of-band by the next telemetry snapshot, not by an ack.
	return tr.Publish(ctx, c.requestTopic(), controlQoS, payload)
}

// Telemetry returns the canonical view. Unlike QueryObjects it never errors on
// an unreachable printer — it reports offline — because it backs UI/status reads
// rather than the snapshot pipeline.
func (c *Client) Telemetry(ctx context.Context) (driver.Telemetry, error) {
	tr, err := c.ensure(ctx)
	if err != nil {
		return offlineTelemetry(), nil
	}
	report, _, connected := tr.LatestReport()
	if !connected || len(report) == 0 {
		return offlineTelemetry(), nil
	}
	return Normalize(report), nil
}

// QueryObjects returns the raw merged Bambu report (top-level "print", "info",
// …). It errors when the printer is unreachable or has not reported yet, so the
// snapshot loop skips it rather than pushing a snapshot that would mark an
// offline printer online (the cloud treats any received snapshot as a liveness
// signal). The agent attaches payload["normalized"] via NormalizeRaw.
func (c *Client) QueryObjects(ctx context.Context) (map[string]any, error) {
	tr, err := c.ensure(ctx)
	if err != nil {
		return nil, fmt.Errorf("bambu: connect: %w", err)
	}
	report, _, connected := tr.LatestReport()
	if !connected {
		return nil, errors.New("bambu: mqtt not connected")
	}
	if len(report) == 0 {
		return nil, errors.New("bambu: no telemetry received yet")
	}
	return report, nil
}

// NormalizeRaw converts a raw report (as returned by QueryObjects) into canonical
// telemetry without I/O. The agent calls it to attach payload["normalized"].
func (c *Client) NormalizeRaw(raw map[string]any) driver.Telemetry {
	if len(raw) == 0 {
		return offlineTelemetry()
	}
	return Normalize(raw)
}

func (c *Client) Pause(ctx context.Context) error { return c.publishPrint(ctx, pauseRequest(c.next())) }
func (c *Client) Resume(ctx context.Context) error {
	return c.publishPrint(ctx, resumeRequest(c.next()))
}
func (c *Client) Cancel(ctx context.Context) error { return c.publishPrint(ctx, stopRequest(c.next())) }

// StartPrint prints a 3MF already uploaded to the printer (see UploadFile). The
// remote-print flow uploads then starts; a bare start_print assumes the file is
// present.
func (c *Client) StartPrint(ctx context.Context, filename string) error {
	if filename == "" {
		return errors.New("bambu: start_print requires a filename")
	}
	return c.publishPrint(ctx, projectFileRequest(c.next(), filename))
}

func (c *Client) UploadFile(ctx context.Context, filename string, content []byte) error {
	if filename == "" {
		return errors.New("bambu: upload_file requires a filename")
	}
	return c.ftpUpload(c.host, c.accessCode, filename, content)
}

func (c *Client) DeleteFile(ctx context.Context, filename string) error {
	if filename == "" {
		return errors.New("bambu: delete_file requires a filename")
	}
	return c.ftpDelete(c.host, c.accessCode, filename)
}

func (c *Client) ListFiles(ctx context.Context) ([]map[string]any, error) {
	return c.ftpList(c.host, c.accessCode)
}

// Home is not exposed: Bambu printers home automatically as part of the print
// preparation sequence and the LAN protocol does not offer a safe standalone
// homing command.
func (c *Client) Home(ctx context.Context, axes ...string) error { return ErrNotSupported }

// GetHistory is not exposed: Bambu has no Moonraker-style history API over the
// LAN protocol; completed-job history is derived cloud-side from telemetry.
func (c *Client) GetHistory(ctx context.Context, limit int) (map[string]any, error) {
	return nil, ErrNotSupported
}

// GetWebcamSnapshot is not exposed yet: Bambu's camera is a model-specific
// stream (P1/A1 chamber protocol on :6000, X1 RTSP) rather than an HTTP
// snapshot. Tracked as a follow-up.
func (c *Client) GetWebcamSnapshot(ctx context.Context) ([]byte, string, error) {
	return nil, "", ErrNotSupported
}

func (c *Client) next() int64 { return c.seq.Add(1) }

func offlineTelemetry() driver.Telemetry {
	return driver.Telemetry{
		SchemaVersion: driver.SchemaVersion,
		Driver:        driver.Bambu,
		State:         driver.StateOffline,
	}
}
