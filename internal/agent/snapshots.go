package agent

import (
	"context"
	"fmt"
	"time"

	"printer-connector/internal/cloud"
	"printer-connector/internal/driver"
)

// defaultPollTimeout caps how long a single printer's QueryObjects call may
// block inside the snapshot loop. Without it, one unresponsive printer
// (Moonraker accepted-but-not-answering, Bambu MQTT hung mid-publish, K1
// asleep behind a half-open TCP socket) could stall the entire snapshot
// cycle until the OS TCP retransmit window expires — multiple minutes —
// during which status.json goes unwritten and every other printer is
// silently missing from its next update. A healthy poll completes in
// 50–300 ms; 10 s leaves ample headroom while keeping the loop's 30 s
// default cadence intact even when several printers time out in a row.
//
// Tests override this via Agent.pollTimeout.
const defaultPollTimeout = 10 * time.Second

func (a *Agent) collectAndPushSnapshots(ctx context.Context) error {
	now := time.Now().UTC()

	attempted := 0
	var snaps []cloud.Snapshot
	// statusEntries accumulates one entry per configured printer (including
	// unreachable ones) for the local status.json written at the end of the cycle.
	printers := a.managedPrinters()
	statusEntries := make([]printerStatus, 0, len(printers))

	timeout := a.pollTimeout
	if timeout == 0 {
		timeout = defaultPollTimeout
	}

	for _, p := range printers {
		mc := a.driverFor(p.PrinterID)
		if mc == nil {
			continue
		}
		attempted++

		pctx, cancel := context.WithTimeout(ctx, timeout)
		payload, err := mc.QueryObjects(pctx)
		cancel()
		if err != nil {
			a.log.Warn("printer query failed", "printer_id", p.PrinterID, "error", err)
			// Record as unreachable/offline so the local status file still lists it.
			// Timeout errors land here too — one wedged printer no longer stalls
			// the loop, and the menu-bar app sees it flip to offline within
			// `timeout` rather than after the OS TCP timeout (~minutes).
			statusEntries = append(statusEntries, buildPrinterStatus(p, driver.Telemetry{}, false))
			continue
		}

		normalized := a.withNormalized(p.PrinterID, payload)
		snaps = append(snaps, cloud.Snapshot{
			PrinterID:  p.PrinterID,
			CapturedAt: now.Format(time.RFC3339),
			Payload:    normalized,
		})

		// Extract the canonical telemetry that withNormalized already computed.
		tel, _ := normalized["normalized"].(driver.Telemetry)
		statusEntries = append(statusEntries, buildPrinterStatus(p, tel, true))
	}

	// Write the local status file regardless of whether the cloud push succeeds —
	// the menu-bar app's local view must not depend on network connectivity.
	if a.cfgPath != "" && len(statusEntries) > 0 {
		if err := writeStatusFile(a.cfgPath, a.cfg, statusEntries); err != nil {
			a.log.Warn("status.json write failed", "error", err)
		}
	}

	if len(snaps) == 0 {
		// No printers configured this cycle: genuinely nothing to do.
		if attempted == 0 {
			return nil
		}
		// We tried every printer and got nothing back. This is the silent-stall
		// case: returning nil here would let the loop report success while
		// pushing no telemetry, leaving the dashboard to mark the printers
		// offline with no explanation. Surface it as an error so the loop logs +
		// backs off and the stall clock is not advanced.
		return fmt.Errorf("no telemetry collected from %d reachable printer(s)", attempted)
	}

	resp, err := a.cloud.PushSnapshots(ctx, cloud.SnapshotsBatchRequest{Snapshots: snaps})
	if err != nil {
		return err
	}
	a.lastSnapshotPushUnix.Store(time.Now().UnixNano())
	a.log.Info("snapshots pushed", "count", len(snaps), "inserted", resp.Inserted)
	return nil
}

func (a *Agent) pushSingleSnapshot(ctx context.Context, printerID int, payload map[string]any) error {
	req := cloud.SnapshotsBatchRequest{
		Snapshots: []cloud.Snapshot{
			{
				PrinterID:  printerID,
				CapturedAt: time.Now().UTC().Format(time.RFC3339),
				Payload:    a.withNormalized(printerID, payload),
			},
		},
	}
	_, err := a.cloud.PushSnapshots(ctx, req)
	return err
}

// withNormalized attaches canonical telemetry at payload["normalized"] alongside
// the raw payload, using the printer's driver to translate the raw response. The
// cloud prefers normalized and falls back to raw, so this is the connector half
// of the no-flag-day migration. The raw payload is left intact.
func (a *Agent) withNormalized(printerID int, payload map[string]any) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	if d := a.driverFor(printerID); d != nil {
		payload["normalized"] = d.NormalizeRaw(payload)
	}
	return payload
}
