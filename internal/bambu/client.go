// Package bambu is a stub Bambu Lab driver. It exists to prove the driver.Driver
// seam holds for a second, very different protocol before the real MQTT/TLS +
// FTPS implementation lands. Control actions return ErrNotImplemented and
// Telemetry reports an offline placeholder; Capabilities already reflects what a
// Bambu printer will support so the cloud UI can be built against it.
package bambu

import (
	"context"
	"errors"

	"printer-connector/internal/driver"
)

// ErrNotImplemented is returned by control actions until the MQTT/FTPS driver
// is implemented.
var ErrNotImplemented = errors.New("bambu driver: not implemented")

// Client is a stub Bambu Lab driver.Driver implementation.
type Client struct {
	host       string
	serial     string
	accessCode string
}

var _ driver.Driver = (*Client)(nil)

// New constructs a stub client. The real driver will dial host over MQTT/TLS and
// FTPS, authenticating with the printer's serial number and access code.
func New(host, serial, accessCode string) *Client {
	return &Client{host: host, serial: serial, accessCode: accessCode}
}

func (c *Client) Capabilities() []string {
	return []string{"pause", "resume", "cancel", "start_print", "remote_print", "webcam"}
}

func (c *Client) Telemetry(ctx context.Context) (driver.Telemetry, error) {
	return driver.Telemetry{
		SchemaVersion: driver.SchemaVersion,
		Driver:        driver.Bambu,
		State:         driver.StateOffline,
	}, nil
}

func (c *Client) QueryObjects(ctx context.Context) (map[string]any, error) {
	return map[string]any{}, nil
}

func (c *Client) Pause(ctx context.Context) error                       { return ErrNotImplemented }
func (c *Client) Resume(ctx context.Context) error                      { return ErrNotImplemented }
func (c *Client) Cancel(ctx context.Context) error                      { return ErrNotImplemented }
func (c *Client) Home(ctx context.Context, axes ...string) error        { return ErrNotImplemented }
func (c *Client) StartPrint(ctx context.Context, filename string) error { return ErrNotImplemented }
func (c *Client) UploadFile(ctx context.Context, filename string, b []byte) error {
	return ErrNotImplemented
}
func (c *Client) DeleteFile(ctx context.Context, filename string) error { return ErrNotImplemented }

func (c *Client) ListFiles(ctx context.Context) ([]map[string]any, error) {
	return nil, ErrNotImplemented
}

func (c *Client) GetHistory(ctx context.Context, limit int) (map[string]any, error) {
	return nil, ErrNotImplemented
}

func (c *Client) GetWebcamSnapshot(ctx context.Context) ([]byte, string, error) {
	return nil, "", ErrNotImplemented
}
