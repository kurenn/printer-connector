package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Client struct {
	baseURL         string
	connectorID     string
	connectorSecret string
	httpClient      *http.Client
	// streamFrameClient uploads live-stream frames. Separate from httpClient so
	// rapid frame pushes reuse their own keep-alive pool and a per-frame timeout
	// generous enough for a small JPEG over a busy uplink without the 5s API cap.
	streamFrameClient *http.Client
	logger            *slog.Logger
	userAgent         string
}

type Options struct {
	BaseURL         string
	ConnectorID     string
	ConnectorSecret string
	Logger          *slog.Logger
	UserAgent       string
}

func New(opts Options) *Client {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}

	streamFrameTransport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}

	return &Client{
		baseURL:         strings.TrimRight(opts.BaseURL, "/"),
		connectorID:     opts.ConnectorID,
		connectorSecret: opts.ConnectorSecret,
		httpClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
		},
		streamFrameClient: &http.Client{
			Timeout:   8 * time.Second,
			Transport: streamFrameTransport,
		},
		logger:    opts.Logger,
		userAgent: opts.UserAgent,
	}
}

func (c *Client) SetCredentials(id, secret string) {
	c.connectorID = id
	c.connectorSecret = secret
}

func (c *Client) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	var out RegisterResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/connectors/register", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Heartbeat(ctx context.Context, hb HeartbeatRequest) error {
	path := fmt.Sprintf("/api/v1/connectors/%s/heartbeat", url.PathEscape(c.connectorID))
	return c.doJSON(ctx, http.MethodPost, path, c.authHeaders(), hb, nil)
}

// RegisterPrinters adopts printers a running connector discovered on the LAN
// after pairing, using its own credentials (no pairing token). Idempotent on the
// cloud side — already-known printers come back with their existing ids.
func (c *Client) RegisterPrinters(ctx context.Context, connectorID string, printers []PrinterInfo) ([]AdoptedPrinter, error) {
	path := fmt.Sprintf("/api/v1/connectors/%s/printers", url.PathEscape(connectorID))
	var out RegisterPrintersResponse
	if err := c.doJSON(ctx, http.MethodPost, path, c.authHeaders(), RegisterPrintersRequest{Printers: printers}, &out); err != nil {
		return nil, err
	}
	return out.Printers, nil
}

func (c *Client) GetCommands(ctx context.Context, connectorID string, limit int) ([]Command, error) {
	path := fmt.Sprintf("/api/v1/connectors/%s/commands?limit=%d", url.PathEscape(connectorID), limit)
	var out []Command
	if err := c.doJSON(ctx, http.MethodGet, path, c.authHeaders(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CompleteCommand(ctx context.Context, commandID StringOrNumber, req CommandCompleteRequest) error {
	path := fmt.Sprintf("/api/v1/commands/%s/complete", url.PathEscape(commandID.String()))
	return c.doJSON(ctx, http.MethodPost, path, c.authHeaders(), req, nil)
}

func (c *Client) PushSnapshots(ctx context.Context, req SnapshotsBatchRequest) (*SnapshotsBatchResponse, error) {
	var out SnapshotsBatchResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/snapshots/batch", c.authHeaders(), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) authHeaders() map[string]string {
	return map[string]string{
		"Authorization":  "Bearer " + c.connectorSecret,
		"X-Connector-Id": c.connectorID,
	}
}

func (c *Client) doJSON(ctx context.Context, method, path string, headers map[string]string, body any, out any) error {
	full := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, full, reqBody)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respB, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respB))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("cloud http %d: %s", resp.StatusCode, msg)
	}

	if out == nil {
		return nil
	}
	if len(respB) == 0 {
		return errors.New("cloud: empty response body")
	}
	if err := json.Unmarshal(respB, out); err != nil {
		return fmt.Errorf("cloud: invalid json: %w", err)
	}
	return nil
}

// UploadBackup uploads a backup archive file to a presigned URL via HTTP PUT.
// This is used for direct upload to cloud storage (S3, GCS, etc).
func (c *Client) UploadBackup(ctx context.Context, presignedURL, filePath string) error {
	// Open backup file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer file.Close()

	// Get file size for Content-Length
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat backup file: %w", err)
	}

	// Create PUT request with file as body
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, presignedURL, file)
	if err != nil {
		return fmt.Errorf("failed to create upload request: %w", err)
	}

	req.Header.Set("Content-Type", "application/gzip")
	req.ContentLength = fileInfo.Size()

	// Execute upload
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error details
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, msg)
	}

	c.logger.Info("backup uploaded successfully",
		"size_bytes", fileInfo.Size(),
		"status", resp.StatusCode,
	)

	return nil
}

// UploadGcode PUTs a fetched G-code file to a presigned cloud URL (the URL
// carries its own signed token, so no auth headers are needed). Mirrors
// UploadBackup but uses a client without the short total timeout — a G-code
// file can be tens of MB and exceed the 5s used for small API calls. The upload
// is bounded by the request context instead.
func (c *Client) UploadGcode(ctx context.Context, presignedURL, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open gcode file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat gcode file: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, presignedURL, file)
	if err != nil {
		return fmt.Errorf("failed to create upload request: %w", err)
	}
	req.Header.Set("Content-Type", "text/x-gcode")
	req.ContentLength = fileInfo.Size()

	client := &http.Client{
		// No total Timeout: a large G-code upload is bounded by ctx, not 5s.
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
			TLSHandshakeTimeout:   3 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       30 * time.Second,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("gcode upload request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("gcode upload failed with status %d: %s", resp.StatusCode, msg)
	}

	c.logger.Info("gcode uploaded successfully",
		"size_bytes", fileInfo.Size(),
		"status", resp.StatusCode,
	)
	return nil
}

// MarkWebcamRequestFailed marks a webcam request as failed on the Rails side,
// preventing the connector's webcam loop from retrying it forever.
func (c *Client) MarkWebcamRequestFailed(ctx context.Context, requestID StringOrNumber, errMsg string) error {
	path := fmt.Sprintf("/api/v1/webcam_requests/%s/fail", url.PathEscape(requestID.String()))
	body := map[string]string{"error_message": errMsg}
	return c.doJSON(ctx, http.MethodPost, path, c.authHeaders(), body, nil)
}

// GetWebcamRequests fetches pending webcam snapshot requests for this connector
func (c *Client) GetWebcamRequests(ctx context.Context, limit int) ([]WebcamRequest, error) {
	path := fmt.Sprintf("/api/v1/connectors/%s/webcam_requests?limit=%d", url.PathEscape(c.connectorID), limit)
	var out []WebcamRequest
	if err := c.doJSON(ctx, http.MethodGet, path, c.authHeaders(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWebcamStreamRequests fetches the printers a browser is actively watching,
// so the connector knows which printers to relay a high-cadence MJPEG feed for.
// Returns an empty slice when no one is watching (the common, cheap case).
func (c *Client) GetWebcamStreamRequests(ctx context.Context) ([]WebcamStreamRequest, error) {
	path := fmt.Sprintf("/api/v1/connectors/%s/webcam_stream", url.PathEscape(c.connectorID))
	var out []WebcamStreamRequest
	if err := c.doJSON(ctx, http.MethodGet, path, c.authHeaders(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UploadWebcamStreamFrame pushes a single live-stream frame for a printer to the
// cloud's fast frame endpoint, which stores only the latest frame (in cache, not
// ActiveStorage) for the browser to poll. Uses the streaming client (no short
// total timeout) since frames are pushed rapidly in a relay loop.
func (c *Client) UploadWebcamStreamFrame(ctx context.Context, printerID int, frame []byte, contentType string) error {
	path := fmt.Sprintf("/api/v1/printers/%d/webcam_stream_frame", printerID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(frame))
	if err != nil {
		return fmt.Errorf("failed to create stream frame request: %w", err)
	}
	for k, v := range c.authHeaders() {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", contentType)
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.streamFrameClient.Do(req)
	if err != nil {
		return fmt.Errorf("stream frame upload failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("stream frame upload failed with status %d: %s", resp.StatusCode, msg)
	}
	return nil
}

// UploadWebcamSnapshot uploads a webcam snapshot image to Rails
// Returns nil on success
func (c *Client) UploadWebcamSnapshot(ctx context.Context, requestID StringOrNumber, printerID int, imageData []byte, contentType string) error {
	path := fmt.Sprintf("/api/v1/webcam_requests/%s/upload", url.PathEscape(requestID.String()))

	// Create request with image as body
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(imageData))
	if err != nil {
		return fmt.Errorf("failed to create upload request: %w", err)
	}

	// Set headers
	for k, v := range c.authHeaders() {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Printer-Id", fmt.Sprintf("%d", printerID))

	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	// Execute upload
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error details
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, msg)
	}

	return nil
}
