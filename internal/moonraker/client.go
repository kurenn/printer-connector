package moonraker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	uiBaseURL  string
	httpClient *http.Client
}

func New(baseURL string, uiPort int) *Client {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}

	// Default to port 80 if not specified (vanilla Klipper default)
	if uiPort == 0 {
		uiPort = 80
	}

	// Build UI base URL from the Moonraker base URL
	// Replace the port from baseURL with uiPort for webcam access
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		// Fallback: just use baseURL for both
		return &Client{
			baseURL:   strings.TrimRight(baseURL, "/"),
			uiBaseURL: strings.TrimRight(baseURL, "/"),
			httpClient: &http.Client{
				Timeout:   5 * time.Second,
				Transport: transport,
			},
		}
	}

	// Build UI URL with the specified UI port
	uiBaseURL := fmt.Sprintf("%s://%s:%d", parsedURL.Scheme, parsedURL.Hostname(), uiPort)

	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		uiBaseURL: strings.TrimRight(uiBaseURL, "/"),
		httpClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
		},
	}
}

func (c *Client) QueryObjects(ctx context.Context) (map[string]any, error) {
	req := map[string]any{
		"objects": map[string]any{
			"print_stats":    nil,
			"virtual_sdcard": nil,
			"extruder":       nil,
			"heater_bed":     nil,
			"toolhead":       nil,
			"pause_resume":   nil,
		},
	}

	var out map[string]any
	if err := c.postJSON(ctx, "/printer/objects/query", req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) Pause(ctx context.Context) error {
	return c.postJSON(ctx, "/printer/print/pause", map[string]any{}, nil)
}

func (c *Client) Resume(ctx context.Context) error {
	return c.postJSON(ctx, "/printer/print/resume", map[string]any{}, nil)
}

func (c *Client) Cancel(ctx context.Context) error {
	return c.postJSON(ctx, "/printer/print/cancel", map[string]any{}, nil)
}

// Home executes the G28 homing command. If axes is empty, homes X Y Z.
// Valid axes are "X", "Y", "Z". Example: Home(ctx, "X", "Y") homes X and Y only.
func (c *Client) Home(ctx context.Context, axes ...string) error {
	gcode := "G28"
	if len(axes) == 0 {
		// Default: home all axes explicitly
		gcode = "G28 X Y Z"
	} else {
		// Home specific axes: G28 X Y
		for _, axis := range axes {
			axis = strings.ToUpper(strings.TrimSpace(axis))
			if axis == "X" || axis == "Y" || axis == "Z" {
				gcode += " " + axis
			}
		}
	}
	req := map[string]any{"script": gcode}
	return c.postJSON(ctx, "/printer/gcode/script", req, nil)
}

func (c *Client) StartPrint(ctx context.Context, filename string) error {
	u := c.baseURL + "/printer/print/start?filename=" + url.QueryEscape(filename)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("moonraker http %d: %s", resp.StatusCode, msg)
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	full := c.baseURL + path
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, full, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

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
		return fmt.Errorf("moonraker http %d: %s", resp.StatusCode, msg)
	}

	if out == nil {
		return nil
	}
	if len(respB) == 0 {
		if mptr, ok := out.(*map[string]any); ok {
			*mptr = map[string]any{}
			return nil
		}
		return nil
	}
	return json.Unmarshal(respB, out)
}

// UploadFile uploads a file to Moonraker
func (c *Client) UploadFile(ctx context.Context, filename string, content []byte) error {
	u := c.baseURL + "/server/files/upload"

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file part
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := part.Write(content); err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	// Add root parameter (upload to gcodes directory)
	if err := writer.WriteField("root", "gcodes"); err != nil {
		return fmt.Errorf("failed to write root field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Execute request
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
		return fmt.Errorf("moonraker http %d: %s", resp.StatusCode, msg)
	}

	return nil
}

// GetHistory fetches print job history from Moonraker
func (c *Client) GetHistory(ctx context.Context, limit int) (map[string]any, error) {
	if limit <= 0 {
		limit = 50 // Default limit
	}
	u := fmt.Sprintf("%s/server/history/list?limit=%d", c.baseURL, limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respB, _ := io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // 5MB limit for history

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respB))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("moonraker http %d: %s", resp.StatusCode, msg)
	}

	var out map[string]any
	if err := json.Unmarshal(respB, &out); err != nil {
		return nil, fmt.Errorf("failed to decode history response: %w", err)
	}
	return out, nil
}

// DeleteFile deletes a file from Moonraker
func (c *Client) DeleteFile(ctx context.Context, filename string) error {
	u := c.baseURL + "/server/files/gcodes/" + url.PathEscape(filename)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
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
		return fmt.Errorf("moonraker http %d: %s", resp.StatusCode, msg)
	}

	return nil
}

// FileInfo represents a file from Moonraker
type FileInfo struct {
	Path         string  `json:"path"`
	Modified     float64 `json:"modified"`
	Size         int64   `json:"size"`
	PrintStartTime *float64 `json:"print_start_time,omitempty"`
}

// ListFiles retrieves the list of files from Moonraker
func (c *Client) ListFiles(ctx context.Context) ([]map[string]any, error) {
	u := c.baseURL + "/server/files/list?root=gcodes"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respB, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respB))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("moonraker http %d: %s", resp.StatusCode, msg)
	}

	var response struct {
		Result []map[string]any `json:"result"`
	}

	if err := json.Unmarshal(respB, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return response.Result, nil
}

// GetWebcamSnapshot retrieves a webcam snapshot from Moonraker
// Returns the image bytes and content type, or an error
func (c *Client) GetWebcamSnapshot(ctx context.Context) ([]byte, string, error) {
	// Try the most common webcam endpoints
	endpoints := []string{
		"/webcam/?action=snapshot",
		"/webcam/snapshot",
		"/server/webcam/snapshot",
	}

	var lastErr error
	for _, endpoint := range endpoints {
		u := c.uiBaseURL + endpoint
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			lastErr = err
			continue
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		// Success - return the image
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// Limit to 10MB for safety
			imageData, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
			if err != nil {
				return nil, "", fmt.Errorf("failed to read snapshot: %w", err)
			}

			contentType := resp.Header.Get("Content-Type")
			if contentType == "" {
				contentType = "image/jpeg" // Default assumption
			}

			return imageData, contentType, nil
		}

		// 404 means try next endpoint
		if resp.StatusCode == 404 {
			continue
		}

		// Other error - read response and return
		respB, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		msg := strings.TrimSpace(string(respB))
		if msg == "" {
			msg = resp.Status
		}
		lastErr = fmt.Errorf("moonraker http %d: %s", resp.StatusCode, msg)
	}

	if lastErr != nil {
		return nil, "", fmt.Errorf("failed to fetch webcam snapshot: %w", lastErr)
	}

	return nil, "", fmt.Errorf("no working webcam endpoint found")
}
