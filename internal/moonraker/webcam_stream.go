package moonraker

import (
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
)

// maxStreamFrameBytes caps a single decoded MJPEG frame. A 10MB ceiling matches
// GetWebcamSnapshot and is far above any real JPEG frame, so a malformed or
// hostile stream can't make the connector buffer unbounded memory per frame.
const maxStreamFrameBytes = 10 << 20

// StreamWebcam opens the printer's MJPEG stream (multipart/x-mixed-replace) and
// invokes onFrame for each JPEG frame it decodes, until ctx is cancelled, the
// stream ends, or onFrame returns an error. It returns the first error that
// stops the loop (nil when ctx ended cleanly or the stream closed normally).
//
// This is the streaming counterpart to GetWebcamSnapshot: it pulls a continuous
// feed from the same camera (the K1's mjpg-streamer at :8080/?action=stream)
// instead of one frame per HTTP round-trip, which is what makes the relayed
// feed smooth rather than a slideshow. It tries the same family of candidate
// hosts/paths as the snapshot path so it works across K1 / vanilla setups.
func (c *Client) StreamWebcam(ctx context.Context, onFrame func(frame []byte, contentType string) error) error {
	var lastErr error
	for _, u := range c.webcamStreamURLs(ctx) {
		err := c.streamFrom(ctx, u, onFrame)
		if err == nil {
			return nil
		}
		// The viewer stopped watching / the relay was torn down: report a clean
		// stop, not whichever candidate happened to be mid-dial when cancelled.
		if ctx.Err() != nil {
			return nil
		}
		// Stop propagating a per-frame (onFrame) error to the next candidate —
		// that means the consumer asked us to stop, not that the URL was bad.
		if frameErr, ok := err.(onFrameError); ok {
			return frameErr.err
		}
		lastErr = err
	}
	if lastErr != nil {
		return fmt.Errorf("failed to open webcam stream: %w", lastErr)
	}
	return fmt.Errorf("no working webcam stream endpoint found")
}

// onFrameError wraps an error returned by the caller's onFrame so StreamWebcam
// can tell "the consumer wants to stop" apart from "this stream URL failed" and
// not fall through to the next candidate.
type onFrameError struct{ err error }

func (e onFrameError) Error() string { return e.err.Error() }

// streamFrom opens a single candidate stream URL and relays its frames. It
// handles the standard multipart/x-mixed-replace MJPEG framing produced by
// mjpg-streamer / ustreamer. If the server doesn't return a multipart boundary
// it returns an error so the caller advances to the next candidate.
func (c *Client) streamFrom(ctx context.Context, u string, onFrame func(frame []byte, contentType string) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	// A stream must not be bounded by the client's short total Timeout, which is
	// meant for one-shot API calls; the request context bounds it instead.
	resp, err := c.streamClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("moonraker http %d for stream", resp.StatusCode)
	}

	mediaType, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return fmt.Errorf("not an mjpeg stream (content-type %q)", resp.Header.Get("Content-Type"))
	}
	boundary := params["boundary"]
	if boundary == "" {
		return fmt.Errorf("mjpeg stream missing multipart boundary")
	}

	mr := multipart.NewReader(resp.Body, boundary)
	for {
		if err := ctx.Err(); err != nil {
			return nil // clean stop on cancellation
		}

		part, err := mr.NextPart()
		if err == io.EOF {
			return nil // stream closed normally
		}
		if err != nil {
			return fmt.Errorf("reading mjpeg part: %w", err)
		}

		frame, err := io.ReadAll(io.LimitReader(part, maxStreamFrameBytes))
		_ = part.Close()
		if err != nil {
			return fmt.Errorf("reading mjpeg frame: %w", err)
		}
		if len(frame) == 0 {
			continue
		}

		ctype := part.Header.Get("Content-Type")
		if ctype == "" {
			ctype = "image/jpeg"
		}
		if err := onFrame(frame, ctype); err != nil {
			return onFrameError{err}
		}
	}
}

// webcamStreamURLs returns ordered, de-duplicated MJPEG stream candidate URLs,
// mirroring webcamSnapshotURLs but using the streaming actions/paths. mjpg-
// streamer serves the continuous stream at ?action=stream (vs ?action=snapshot
// for a single frame); the K1's camera lives directly on :8080.
func (c *Client) webcamStreamURLs(ctx context.Context) []string {
	var urls []string
	seen := map[string]bool{}
	add := func(u string) {
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		urls = append(urls, u)
	}

	// (1) Derive a stream URL from whatever snapshot URL Moonraker reports: the
	// stream is the same camera with ?action=snapshot swapped for ?action=stream.
	if path := c.discoverSnapshotPath(ctx); path != "" {
		streamPath := strings.Replace(path, "action=snapshot", "action=stream", 1)
		if strings.HasPrefix(streamPath, "http://") || strings.HasPrefix(streamPath, "https://") {
			add(streamPath)
		} else {
			rel := "/" + strings.TrimLeft(streamPath, "/")
			add(c.uiBaseURL + rel)
			if c.host != "" {
				add(fmt.Sprintf("http://%s:8080%s", c.host, rel))
			}
		}
	}

	// (2) mjpg-streamer default — the K1 serves the stream here.
	if c.host != "" {
		add(fmt.Sprintf("http://%s:8080/?action=stream", c.host))
		add(fmt.Sprintf("http://%s:8080/webcam/?action=stream", c.host))
	}

	// (3) Legacy proxied paths on the UI host (vanilla Mainsail/Fluidd).
	add(c.uiBaseURL + "/webcam/?action=stream")
	add(c.uiBaseURL + "/webcam/stream")

	return urls
}
