package agent

import (
	"context"
	"time"

	"printer-connector/internal/cloud"
)

func (a *Agent) sendHeartbeat(ctx context.Context) error {
	hb := cloud.HeartbeatRequest{}
	hb.Status.UptimeSeconds = int64(time.Since(a.startedAt).Seconds())
	hb.Status.Version = a.version

	for _, p := range a.cfg.Moonraker {
		reachable := false
		mc := a.moons[p.PrinterID]
		if mc != nil {
			_, err := mc.QueryObjects(ctx)
			reachable = (err == nil)
		}
		hb.Printers = append(hb.Printers, cloud.HeartbeatPrinter{
			PrinterID: p.PrinterID,
			Reachable: reachable,
		})
	}

	return a.cloud.Heartbeat(ctx, hb)
}
