package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"printer-connector/internal/cloud"
	"printer-connector/internal/config"
	"printer-connector/internal/driver"
)

// fakeStreamer feeds a fixed set of frames to runStreamRelay as fast as the
// relay reads them, satisfying the webcamStreamer capability for tests.
type fakeStreamer struct {
	frames [][]byte
	delay  time.Duration // pause between frames, to exercise throttling
}

func (f *fakeStreamer) StreamWebcam(ctx context.Context, onFrame func(frame []byte, contentType string) error) error {
	for {
		for _, fr := range f.frames {
			if ctx.Err() != nil {
				return nil
			}
			if err := onFrame(fr, "image/jpeg"); err != nil {
				return err
			}
			if f.delay > 0 {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(f.delay):
				}
			}
		}
	}
}

// runStreamRelay must upload frames to the cloud's stream-frame endpoint and
// stop once the viewer's deadline passes.
func TestRunStreamRelayUploadsFramesAndStopsAtDeadline(t *testing.T) {
	var uploads int64

	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/api/v1/printers/1/webcam_stream_frame" {
			atomic.AddInt64(&uploads, 1)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer cloudSrv.Close()

	a := &Agent{
		log:   discardLogger(),
		cfg:   &config.Config{ConnectorID: "1"},
		cloud: cloud.New(cloud.Options{BaseURL: cloudSrv.URL, ConnectorID: "1", Logger: discardLogger()}),
	}

	streamer := &fakeStreamer{frames: [][]byte{{0xFF, 0xD8, 0x01}}, delay: 10 * time.Millisecond}
	deadline := time.Now().Add(150 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		a.runStreamRelay(context.Background(), 1, streamer, func() time.Time { return deadline })
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runStreamRelay did not stop at the deadline")
	}

	got := atomic.LoadInt64(&uploads)
	if got == 0 {
		t.Fatal("no frames were uploaded")
	}
	// With an 8fps cap (~125ms between sends) over a ~150ms window, only a small
	// number of frames should be sent even though the streamer offers many more.
	if got > 5 {
		t.Fatalf("throttling failed: %d frames uploaded in ~150ms, expected the fps cap to limit it", got)
	}
}

// The stream loop must skip printers whose driver can't stream (no MJPEG
// capability) and not panic — the snapshot relay covers those printers.
func TestWebcamStreamLoopSkipsNonStreamingDriver(t *testing.T) {
	served := make(chan struct{}, 1)
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/connectors/1/webcam_stream" {
			w.Header().Set("Content-Type", "application/json")
			// Request streaming for printer 1, which has no streaming driver here.
			_, _ = w.Write([]byte(`[{"printer_id":1,"expires_in_ms":5000}]`))
			select {
			case served <- struct{}{}:
			default:
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer cloudSrv.Close()

	a := &Agent{
		log:     discardLogger(),
		cfg:     &config.Config{ConnectorID: "1"},
		cloud:   cloud.New(cloud.Options{BaseURL: cloudSrv.URL, ConnectorID: "1", Logger: discardLogger()}),
		drivers: map[int]driver.Driver{}, // no driver -> no streamer; must be skipped
	}

	ctx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan error, 1)
	go func() { loopDone <- a.webcamStreamLoop(ctx) }()

	select {
	case <-served:
	case <-time.After(2 * time.Second):
		t.Fatal("stream poll was never served")
	}

	// Give the loop a moment to process the (skipped) request, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-loopDone:
		if err != context.Canceled {
			t.Fatalf("loop ended with %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webcamStreamLoop did not exit on cancel")
	}
}
