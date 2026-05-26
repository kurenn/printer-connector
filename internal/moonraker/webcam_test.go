package moonraker

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Hostname()
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

func TestDiscoverSnapshotPathReadsMoonraker(t *testing.T) {
	moon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/server/webcams/list" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"result":{"webcams":[{"name":"Camera","snapshot_url":"/webcam/?action=snapshot"}]}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer moon.Close()

	c := New(moon.URL, 80)
	if got := c.discoverSnapshotPath(context.Background()); got != "/webcam/?action=snapshot" {
		t.Fatalf("discoverSnapshotPath = %q, want /webcam/?action=snapshot", got)
	}

	// No camera / older Moonraker → empty, never an error.
	none := httptest.NewServer(http.HandlerFunc(http.NotFound))
	defer none.Close()
	if got := New(none.URL, 80).discoverSnapshotPath(context.Background()); got != "" {
		t.Fatalf("expected empty path when Moonraker has no webcam list, got %q", got)
	}
}

// The candidate list must include the mjpg-streamer default port (8080) where
// the K1 actually serves frames — the bug was that we only ever tried the
// proxied /webcam path on the UI port.
func TestWebcamSnapshotURLsIncludesStreamerPort(t *testing.T) {
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

	urls := New(moon.URL, 80).webcamSnapshotURLs(context.Background())

	// The K1-critical candidate.
	if !contains(urls, "http://"+host+":8080/?action=snapshot") {
		t.Fatalf("missing mjpg-streamer default candidate; got %v", urls)
	}
	// The Moonraker-reported path, proxied on the UI host (vanilla setups).
	if !contains(urls, "http://"+host+":80/webcam/?action=snapshot") {
		t.Fatalf("missing discovered path on UI host; got %v", urls)
	}
	// De-duplicated.
	seen := map[string]bool{}
	for _, u := range urls {
		if seen[u] {
			t.Fatalf("duplicate candidate %q in %v", u, urls)
		}
		seen[u] = true
	}
}

// End-to-end: when Moonraker reports an absolute snapshot URL, GetWebcamSnapshot
// fetches it and returns the bytes (exercises discovery + fetch + candidate).
func TestGetWebcamSnapshotFollowsDiscoveredAbsoluteURL(t *testing.T) {
	cam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00})
	}))
	defer cam.Close()

	moon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/server/webcams/list" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"result":{"webcams":[{"snapshot_url":"`+cam.URL+`/snap"}]}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer moon.Close()

	img, ctype, err := New(moon.URL, 80).GetWebcamSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GetWebcamSnapshot: %v", err)
	}
	if ctype != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", ctype)
	}
	if len(img) != 5 {
		t.Fatalf("image bytes = %d, want 5", len(img))
	}
}
