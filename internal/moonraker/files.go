package moonraker

import (
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

// FileEntry is a file reported by Moonraker's file-manager list endpoint.
type FileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ListRootFiles returns the files under a Moonraker file root such as "config",
// "logs", or "gcodes" — GET /server/files/list?root=<root>. These roots ship
// with a stock Moonraker, so nothing extra is installed on the printer.
func (c *Client) ListRootFiles(ctx context.Context, root string) ([]FileEntry, error) {
	u := c.baseURL + "/server/files/list?root=" + url.QueryEscape(root)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list files (root=%s): %w", root, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list files (root=%s): unexpected status %d", root, resp.StatusCode)
	}
	var out struct {
		Result []FileEntry `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode file list (root=%s): %w", root, err)
	}
	return out.Result, nil
}

// DownloadFile streams a single file from a Moonraker root into w —
// GET /server/files/<root>/<path>.
func (c *Client) DownloadFile(ctx context.Context, root, path string, w io.Writer) error {
	u := c.baseURL + "/server/files/" + url.PathEscape(root) + "/" + escapeFilePath(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s/%s: %w", root, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s/%s: unexpected status %d", root, path, resp.StatusCode)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("stream %s/%s: %w", root, path, err)
	}
	return nil
}

// DownloadFileStream streams a single file from a Moonraker root into w, like
// DownloadFile, but without the shared client's short total request timeout —
// a G-code file can be tens of MB and take longer than the 5s used for small
// API calls. The caller bounds the operation with the request context's
// deadline instead. The connection/header timeouts still apply.
func (c *Client) DownloadFileStream(ctx context.Context, root, path string, w io.Writer) error {
	u := c.baseURL + "/server/files/" + url.PathEscape(root) + "/" + escapeFilePath(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	client := &http.Client{
		// No total Timeout: bounded by ctx so large transfers aren't cut off.
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
			ResponseHeaderTimeout: 5 * time.Second,
			IdleConnTimeout:       30 * time.Second,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s/%s: %w", root, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s/%s: unexpected status %d", root, path, resp.StatusCode)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("stream %s/%s: %w", root, path, err)
	}
	return nil
}

// escapeFilePath URL-escapes each path segment while preserving "/" separators.
func escapeFilePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}
