package agent

import (
	"context"
	"time"

	"printer-connector/internal/util"
)

// webcamStreamer is the optional capability a driver advertises if it can
// deliver a continuous MJPEG feed (Moonraker/mjpg-streamer does; Bambu does
// not). The stream loop type-asserts for it, so adding the high-cadence relay
// did NOT change the core Driver contract or force every protocol to implement
// streaming — camera-less / non-MJPEG printers simply never enter stream mode.
type webcamStreamer interface {
	StreamWebcam(ctx context.Context, onFrame func(frame []byte, contentType string) error) error
}

// streamPollInterval is how often the connector asks the cloud which printers a
// browser is watching. Short, because a viewer should see the feed turn smooth
// within ~1s of opening the page; the request is tiny and returns empty (the
// common case) when no one is watching.
const streamPollInterval = 1 * time.Second

// maxStreamFPS caps how many frames per second the connector relays per printer.
// ~8fps reads as smooth-ish video while bounding LAN + uplink + cloud load; the
// camera itself (and the uplink) is usually the real ceiling. Pushing every
// frame from a 30fps camera would swamp the connector's upstream link.
const maxStreamFPS = 8

// webcamLoop's sibling: drives the live MJPEG stream relay. It polls the cloud
// for printers a browser is actively watching and, for each, opens the printer's
// MJPEG stream and relays frames at a capped cadence for the duration the viewer
// requested. One in-flight relay per printer; a new poll that still lists the
// printer extends the window rather than starting a second relay.
func (a *Agent) webcamStreamLoop(ctx context.Context) error {
	tick := time.NewTicker(streamPollInterval)
	defer tick.Stop()

	bo := util.NewBackoff(1*time.Second, 30*time.Second)

	// active tracks the cancel func + deadline of the in-flight relay per printer.
	type relay struct {
		cancel   context.CancelFunc
		deadline time.Time
	}
	active := map[int]*relay{}

	// Always cancel any in-flight relays when the loop exits.
	defer func() {
		for _, r := range active {
			r.cancel()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}

		reqs, err := a.cloud.GetWebcamStreamRequests(ctx)
		if err != nil {
			a.log.Debug("webcam stream poll failed", "error", err)
			if werr := util.Wait(ctx, bo.Next()); werr != nil {
				return werr
			}
			continue
		}
		bo.Reset()

		now := time.Now()
		for _, req := range reqs {
			window := time.Duration(req.ExpiresInMs) * time.Millisecond
			if window <= 0 {
				continue
			}
			deadline := now.Add(window)

			if r, ok := active[req.PrinterID]; ok {
				// Already streaming this printer — just extend the window.
				if deadline.After(r.deadline) {
					r.deadline = deadline
				}
				continue
			}

			streamer, ok := a.drivers[req.PrinterID].(webcamStreamer)
			if !ok {
				continue // printer has no MJPEG stream capability; snapshot path covers it
			}

			relayCtx, cancel := context.WithCancel(ctx)
			r := &relay{cancel: cancel, deadline: deadline}
			active[req.PrinterID] = r
			go a.runStreamRelay(relayCtx, req.PrinterID, streamer, func() time.Time { return r.deadline })
		}

		// Reap finished/cancelled relays: a relay whose goroutine has exited (its
		// deadline passed and it self-cancelled) is removed so the next matching
		// poll starts a fresh one. We detect exit by the deadline being in the
		// past AND the context already cancelled.
		for id, r := range active {
			if r.deadline.Before(now) {
				r.cancel()
				delete(active, id)
			}
		}
	}
}

// runStreamRelay opens the printer's MJPEG stream and relays frames to the cloud
// at a capped cadence until the viewer's deadline passes (deadlineFn is read
// each frame so an extended window keeps it alive) or the context is cancelled.
// A frame that arrives sooner than the cadence allows is dropped — we relay the
// freshest frame at a steady rate, not every frame the camera emits.
func (a *Agent) runStreamRelay(ctx context.Context, printerID int, streamer webcamStreamer, deadlineFn func() time.Time) {
	a.log.Info("webcam stream relay started", "printer_id", printerID)

	minInterval := time.Second / maxStreamFPS
	var lastSent time.Time

	err := streamer.StreamWebcam(ctx, func(frame []byte, contentType string) error {
		now := time.Now()
		if now.After(deadlineFn()) {
			return context.Canceled // viewer stopped watching — end the stream
		}
		if !lastSent.IsZero() && now.Sub(lastSent) < minInterval {
			return nil // throttle: drop this frame, keep the stream open
		}
		lastSent = now

		// Bound a single frame upload so a slow upload can't wedge the relay.
		upCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := a.cloud.UploadWebcamStreamFrame(upCtx, printerID, frame, contentType); err != nil {
			a.log.Debug("webcam stream frame upload failed", "printer_id", printerID, "error", err)
			// A transient cloud hiccup shouldn't tear down the stream; keep going.
		}
		return nil
	})

	if err != nil && ctx.Err() == nil {
		a.log.Warn("webcam stream relay ended with error", "printer_id", printerID, "error", err)
	} else {
		a.log.Info("webcam stream relay stopped", "printer_id", printerID)
	}
}
