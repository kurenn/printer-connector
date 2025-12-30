package agent

import (
	"context"
	"time"

	"printer-connector/internal/cloud"
)

func (a *Agent) collectAndPushSnapshots(ctx context.Context) error {
	now := time.Now().UTC()

	var snaps []cloud.Snapshot
	for _, p := range a.cfg.Moonraker {
		mc := a.moons[p.PrinterID]
		if mc == nil {
			continue
		}

		payload, err := mc.QueryObjects(ctx)
		if err != nil {
			a.log.Warn("moonraker query failed", "printer_id", p.PrinterID, "error", err)
			continue
		}

		snaps = append(snaps, cloud.Snapshot{
			PrinterID:  p.PrinterID,
			CapturedAt: now.Format(time.RFC3339),
			Payload:    payload,
		})
	}

	if len(snaps) == 0 {
		return nil
	}

	resp, err := a.cloud.PushSnapshots(ctx, cloud.SnapshotsBatchRequest{Snapshots: snaps})
	if err != nil {
		return err
	}
	a.log.Info("snapshots pushed", "count", len(snaps), "inserted", resp.Inserted)
	return nil
}

func (a *Agent) pushSingleSnapshot(ctx context.Context, printerID int, payload map[string]any) error {
	req := cloud.SnapshotsBatchRequest{
		Snapshots: []cloud.Snapshot{
			{
				PrinterID:  printerID,
				CapturedAt: time.Now().UTC().Format(time.RFC3339),
				Payload:    payload,
			},
		},
	}
	_, err := a.cloud.PushSnapshots(ctx, req)
	return err
}
