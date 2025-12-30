package moonraker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *Client {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
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
