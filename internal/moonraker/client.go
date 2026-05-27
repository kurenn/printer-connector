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
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	baseURL    string
	uiBaseURL  string
	host       string // bare hostname/IP from baseURL, for building camera URLs
	httpClient *http.Client

	// Filament-sensor object names (e.g. "filament_switch_sensor runout") are
	// printer-specific, so we discover them once via /printer/objects/list and
	// cache them. Guarded by mu; resolved is only set true on a successful
	// discovery so a transient failure is retried on the next query.
	//
	// Thermal extras (extruder1..N, temperature_sensor *, heater_generic *,
	// temperature_fan *) follow the same pattern — they are discovered together
	// with filament sensors in a single /printer/objects/list call and cached
	// under thermalExtras / thermalsResolved.
	mu               sync.Mutex
	filamentSensors  []string
	sensorsResolved  bool
	thermalExtras    []string
	thermalsResolved bool
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
		host:      parsedURL.Hostname(),
		httpClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
		},
	}
}

// streamClient returns an HTTP client suitable for a long-lived MJPEG stream:
// it keeps the dial/header timeouts but drops the short total Timeout (which is
// meant for one-shot API calls and would abort the stream after 5s). The stream
// is instead bounded by the request context the caller passes.
func (c *Client) streamClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
			ResponseHeaderTimeout: 5 * time.Second,
			IdleConnTimeout:       30 * time.Second,
		},
	}
}

func (c *Client) QueryObjects(ctx context.Context) (map[string]any, error) {
	objects := map[string]any{
		"print_stats":    nil,
		"virtual_sdcard": nil,
		"extruder":       nil,
		"heater_bed":     nil,
		"toolhead":       nil,
		"pause_resume":   nil,
	}
	// Include any filament-runout sensors so snapshots carry filament_detected.
	// Discovery is best-effort: if it fails we still report the core objects.
	for _, name := range c.filamentSensorObjects(ctx) {
		objects[name] = nil
	}
	// Include extra thermal objects (additional extruders, temperature_sensor *,
	// heater_generic *, temperature_fan *) so Rails receives chamber/toolhead
	// temperatures. Discovery is best-effort — failures just omit the extras.
	for _, name := range c.thermalExtraObjects(ctx) {
		objects[name] = nil
	}

	req := map[string]any{"objects": objects}

	var out map[string]any
	if err := c.postJSON(ctx, "/printer/objects/query", req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// filamentSensorObjects returns the printer's filament_switch_sensor /
// filament_motion_sensor object names, discovering and caching them on first
// use. On discovery failure it returns nil (the caller proceeds without them)
// and leaves the cache unresolved so the next query retries.
func (c *Client) filamentSensorObjects(ctx context.Context) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sensorsResolved {
		return c.filamentSensors
	}
	c.discoverObjectsLocked(ctx)
	return c.filamentSensors
}

// thermalExtraObjects returns the printer's additional thermal object names:
// extra extruders (extruder1..N), temperature_sensor *, heater_generic *, and
// temperature_fan *. These are discovered and cached on first use using the same
// /printer/objects/list call as filament sensors. On discovery failure it
// returns nil and leaves the cache unresolved so the next query retries.
func (c *Client) thermalExtraObjects(ctx context.Context) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.thermalsResolved {
		return c.thermalExtras
	}
	c.discoverObjectsLocked(ctx)
	return c.thermalExtras
}

// discoverObjectsLocked calls /printer/objects/list and populates both the
// filament-sensor cache and the thermal-extras cache in a single HTTP round
// trip. It must be called with c.mu held. On failure the caches are left
// unresolved so the next caller retries.
func (c *Client) discoverObjectsLocked(ctx context.Context) {
	names, err := c.listObjects(ctx)
	if err != nil {
		return
	}

	var sensors []string
	var extras []string
	for _, n := range names {
		if strings.HasPrefix(n, "filament_switch_sensor ") || strings.HasPrefix(n, "filament_motion_sensor ") {
			sensors = append(sensors, n)
		} else if isThermalExtra(n) {
			extras = append(extras, n)
		}
	}
	c.filamentSensors = sensors
	c.sensorsResolved = true
	c.thermalExtras = extras
	c.thermalsResolved = true
}

// isThermalExtra reports whether a Moonraker object name represents a thermal
// object that should be included in snapshots beyond the always-queried
// "extruder" and "heater_bed". Matches:
//   - extruder1, extruder2, … (multi-extruder setups)
//   - temperature_sensor <name>  (e.g. "temperature_sensor chamber")
//   - heater_generic <name>      (e.g. "heater_generic chamber_heater")
//   - temperature_fan <name>     (e.g. "temperature_fan mcu_fan")
func isThermalExtra(name string) bool {
	if len(name) > len("extruder") && strings.HasPrefix(name, "extruder") {
		rest := name[len("extruder"):]
		for _, ch := range rest {
			if ch < '0' || ch > '9' {
				goto notExtruder
			}
		}
		return true
	notExtruder:
	}
	return strings.HasPrefix(name, "temperature_sensor ") ||
		strings.HasPrefix(name, "heater_generic ") ||
		strings.HasPrefix(name, "temperature_fan ")
}

// listObjects fetches the list of available Klipper object names from Moonraker.
func (c *Client) listObjects(ctx context.Context) ([]string, error) {
	u := c.baseURL + "/printer/objects/list"
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

	respB, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB cap
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respB))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("moonraker http %d: %s", resp.StatusCode, msg)
	}

	var parsed struct {
		Result struct {
			Objects []string `json:"objects"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respB, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode objects list: %w", err)
	}
	return parsed.Result.Objects, nil
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

// UploadFileReader streams a file to Moonraker's file API without buffering it
// in memory (the source can be tens/hundreds of MB), and optionally asks
// Moonraker to start the print immediately via the `print` form field — so an
// upload+autostart is a single atomic call with no separate StartPrint race.
//
// Used by the slicer-upload flow, where the bytes arrive from a cloud download
// rather than as in-memory base64. The body is assembled through an io.Pipe so
// the multipart stream flows straight from the reader to the socket.
func (c *Client) UploadFileReader(ctx context.Context, filename string, r io.Reader, print bool) error {
	u := c.baseURL + "/server/files/upload"

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		var err error
		defer func() { pw.CloseWithError(err) }()

		part, e := writer.CreateFormFile("file", filename)
		if e != nil {
			err = e
			return
		}
		if _, e = io.Copy(part, r); e != nil {
			err = e
			return
		}
		if e = writer.WriteField("root", "gcodes"); e != nil {
			err = e
			return
		}
		if e = writer.WriteField("print", strconv.FormatBool(print)); e != nil {
			err = e
			return
		}
		err = writer.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// A client without the 5s total timeout: a large upload is bounded by ctx.
	client := &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       30 * time.Second,
		},
	}

	resp, err := client.Do(req)
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
	Path           string   `json:"path"`
	Modified       float64  `json:"modified"`
	Size           int64    `json:"size"`
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
	var lastErr error
	for _, u := range c.webcamSnapshotURLs(ctx) {
		img, ctype, err := c.fetchSnapshot(ctx, u)
		if err == nil {
			return img, ctype, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, "", fmt.Errorf("failed to fetch webcam snapshot: %w", lastErr)
	}
	return nil, "", fmt.Errorf("no working webcam endpoint found")
}

// fetchSnapshot GETs a single candidate URL, returning the image on a 2xx and
// an error otherwise (so the caller advances to the next candidate).
func (c *Client) fetchSnapshot(ctx context.Context, u string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		img, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB cap
		if err != nil {
			return nil, "", fmt.Errorf("failed to read snapshot: %w", err)
		}
		ctype := resp.Header.Get("Content-Type")
		if ctype == "" {
			ctype = "image/jpeg"
		}
		return img, ctype, nil
	}

	respB, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	msg := strings.TrimSpace(string(respB))
	if msg == "" {
		msg = resp.Status
	}
	return nil, "", fmt.Errorf("moonraker http %d: %s", resp.StatusCode, msg)
}

// webcamSnapshotURLs returns ordered, de-duplicated snapshot URLs to try. It
// blends three sources so it works across setups:
//  1. the snapshot_url Moonraker actually reports (/server/webcams/list);
//  2. the mjpg-streamer default port 8080, where many Klipper cams — including
//     the Creality K1, whose camera lives at :8080/?action=snapshot rather than
//     a proxied /webcam/ path — serve frames directly;
//  3. the legacy proxied paths on the UI host, for vanilla Mainsail/Fluidd.
func (c *Client) webcamSnapshotURLs(ctx context.Context) []string {
	var urls []string
	seen := map[string]bool{}
	add := func(u string) {
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		urls = append(urls, u)
	}

	// (1) Whatever Moonraker says the configured camera's snapshot URL is.
	if path := c.discoverSnapshotPath(ctx); path != "" {
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			add(path) // absolute — use verbatim
		} else {
			rel := "/" + strings.TrimLeft(path, "/")
			add(c.uiBaseURL + rel) // proxied via the UI host (vanilla setups)
			if c.host != "" {
				add(fmt.Sprintf("http://%s:8080%s", c.host, rel)) // same path on the streamer port
			}
		}
	}

	// (2) mjpg-streamer default — the K1 serves frames here.
	if c.host != "" {
		add(fmt.Sprintf("http://%s:8080/?action=snapshot", c.host))
		add(fmt.Sprintf("http://%s:8080/webcam/?action=snapshot", c.host))
	}

	// (3) Legacy proxied paths on the UI host (back-compat with the old logic).
	add(c.uiBaseURL + "/webcam/?action=snapshot")
	add(c.uiBaseURL + "/webcam/snapshot")
	add(c.uiBaseURL + "/server/webcam/snapshot")

	return urls
}

// discoverSnapshotPath asks Moonraker for the configured webcam's snapshot URL.
// Best-effort: any error (no camera, older Moonraker) returns "".
func (c *Client) discoverSnapshotPath(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/server/webcams/list", nil)
	if err != nil {
		return ""
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}
	var out struct {
		Result struct {
			Webcams []struct {
				SnapshotURL string `json:"snapshot_url"`
			} `json:"webcams"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return ""
	}
	for _, w := range out.Result.Webcams {
		if w.SnapshotURL != "" {
			return w.SnapshotURL
		}
	}
	return ""
}
