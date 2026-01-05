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
	logger          *slog.Logger
	userAgent       string
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

	return &Client{
		baseURL:         strings.TrimRight(opts.BaseURL, "/"),
		connectorID:     opts.ConnectorID,
		connectorSecret: opts.ConnectorSecret,
		httpClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
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
