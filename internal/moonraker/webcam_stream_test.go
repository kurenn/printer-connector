package moonraker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"
)

// portOf extracts the port from an httptest server URL, so New() builds a UI
// base URL pointing back at the same test server.
func portOf(t *testing.T, raw string) int {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("port of %q: %v", raw, err)
	}
	return p
}

// writeMJPEG serves a small multipart/x-mixed-replace stream of `frames` JPEG
// payloads, mimicking mjpg-streamer's framing. It blocks until the client
// disconnects (ctx cancelled) after the last frame, like a real stream.
func writeMJPEG(w http.ResponseWriter, r *http.Request, frames [][]byte) {
	const boundary = "boundarydonotcross"
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+boundary)
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	for _, f := range frames {
		fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", boundary, len(f))
		_, _ = w.Write(f)
		_, _ = io.WriteString(w, "\r\n")
		if fl != nil {
			fl.Flush()
		}
	}
	// A multipart part's body is only terminated by the *next* boundary, so emit
	// a trailing boundary delimiter — otherwise the reader blocks waiting to
	// terminate the final frame (real cameras stream continuously, so this only
	// matters for a finite test feed).
	fmt.Fprintf(w, "--%s\r\n", boundary)
	if fl != nil {
		fl.Flush()
	}
	// Hold the connection open until the client goes away, like a live camera.
	<-r.Context().Done()
}

func TestStreamWebcamRelaysFrames(t *testing.T) {
	want := [][]byte{
		{0xFF, 0xD8, 0x01},
		{0xFF, 0xD8, 0x02, 0x03},
		{0xFF, 0xD8, 0x04},
	}

	var cam *httptest.Server
	cam = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Report an absolute snapshot URL pointing back at ourselves so the
		// derived stream URL (action swapped) is the FIRST candidate tried and
		// connects immediately — avoids the slow :8080 dial fall-through.
		if r.URL.Path == "/server/webcams/list" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"result":{"webcams":[{"snapshot_url":"`+cam.URL+`/?action=snapshot"}]}}`)
			return
		}
		if r.URL.Query().Get("action") == "stream" {
			writeMJPEG(w, r, want)
			return
		}
		http.NotFound(w, r)
	}))
	defer cam.Close()

	c := New(cam.URL, portOf(t, cam.URL))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var mu sync.Mutex
	var got [][]byte
	err := c.StreamWebcam(ctx, func(frame []byte, contentType string) error {
		if contentType != "image/jpeg" {
			t.Errorf("content type = %q, want image/jpeg", contentType)
		}
		mu.Lock()
		cp := append([]byte(nil), frame...)
		got = append(got, cp)
		stop := len(got) >= len(want)
		mu.Unlock()
		if stop {
			return io.EOF // consumer asks to stop after the expected frames
		}
		return nil
	})
	// io.EOF returned from onFrame surfaces as the loop's stop signal, not nil,
	// because we deliberately ask to stop; that's fine — assert we got the frames.
	if err != nil && err != io.EOF {
		t.Fatalf("StreamWebcam: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) < len(want) {
		t.Fatalf("got %d frames, want >= %d", len(got), len(want))
	}
	for i := range want {
		if string(got[i]) != string(want[i]) {
			t.Fatalf("frame %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// A non-multipart response (e.g. a snapshot endpoint, or an error page) must be
// rejected so StreamWebcam falls through to the next candidate instead of
// treating HTML/a single JPEG as a stream.
func TestStreamFromRejectsNonMultipart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0x00})
	}))
	defer srv.Close()

	c := New(srv.URL, portOf(t, srv.URL))
	err := c.streamFrom(context.Background(), srv.URL+"/?action=stream", func([]byte, string) error { return nil })
	if err == nil {
		t.Fatal("expected error for non-multipart response, got nil")
	}
}

// The stream candidate list must include the K1's mjpg-streamer stream endpoint
// and derive a stream URL from a discovered snapshot URL by swapping the action.
func TestWebcamStreamURLs(t *testing.T) {
	moon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/server/webcams/list" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"result":{"webcams":[{"snapshot_url":"/webcam/?action=snapshot"}]}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer moon.Close()
	host := hostOf(t, moon.URL)

	urls := New(moon.URL, 80).webcamStreamURLs(context.Background())

	if !contains(urls, "http://"+host+":8080/?action=stream") {
		t.Fatalf("missing mjpg-streamer stream candidate; got %v", urls)
	}
	// Discovered snapshot path → stream path (action swapped), on the UI host.
	if !contains(urls, "http://"+host+":80/webcam/?action=stream") {
		t.Fatalf("missing derived stream path from discovery; got %v", urls)
	}
	seen := map[string]bool{}
	for _, u := range urls {
		if seen[u] {
			t.Fatalf("duplicate candidate %q in %v", u, urls)
		}
		seen[u] = true
	}
}

// A cancelled context stops the stream cleanly (nil error), so a relay whose
// viewer stopped watching tears down without surfacing a spurious failure.
func TestStreamWebcamStopsOnContextCancel(t *testing.T) {
	cam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMJPEG(w, r, [][]byte{{0xFF, 0xD8, 0x01}})
	}))
	defer cam.Close()

	c := New(cam.URL, portOf(t, cam.URL))
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- c.StreamWebcam(ctx, func([]byte, string) error { return nil })
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil error on cancel, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StreamWebcam did not return after context cancel")
	}
}
