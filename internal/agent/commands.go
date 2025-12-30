package agent

import (
	"context"
	"fmt"
	"time"

	"printer-connector/internal/cloud"
)

func (a *Agent) pollAndExecuteCommands(ctx context.Context) error {
	cmds, err := a.cloud.GetCommands(ctx, a.cfg.ConnectorID, 20)
	if err != nil {
		return err
	}
	if len(cmds) == 0 {
		return nil
	}

	for _, cmd := range cmds {
		start := time.Now()
		a.log.Info("executing command", "command_id", cmd.ID, "printer_id", cmd.PrinterID, "action", cmd.Action)

		mc := a.moons[cmd.PrinterID]
		if mc == nil {
			_ = a.cloud.CompleteCommand(ctx, cmd.ID, cloud.CommandCompleteRequest{
				Status:       "failed",
				ErrorMessage: fmt.Sprintf("unknown printer_id %d", cmd.PrinterID),
				Result:       map[string]any{"printer_id": cmd.PrinterID},
			})
			continue
		}

		var execErr error
		result := map[string]any{"action": cmd.Action}

		switch cmd.Action {
		case "pause":
			execErr = mc.Pause(ctx)
		case "resume":
			execErr = mc.Resume(ctx)
		case "cancel":
			execErr = mc.Cancel(ctx)
		case "start_print":
			filename, _ := cmd.Params["filename"].(string)
			if filename == "" {
				execErr = fmt.Errorf("missing params.filename for start_print")
			} else {
				result["filename"] = filename
				execErr = mc.StartPrint(ctx, filename)
			}
		default:
			execErr = fmt.Errorf("unsupported action: %s", cmd.Action)
		}

		if execErr != nil {
			a.log.Warn("command failed", "command_id", cmd.ID, "error", execErr)
			_ = a.cloud.CompleteCommand(ctx, cmd.ID, cloud.CommandCompleteRequest{
				Status:       "failed",
				ErrorMessage: execErr.Error(),
				Result:       result,
			})
			continue
		}

		if payload, snapErr := mc.QueryObjects(ctx); snapErr == nil {
			result["post_snapshot"] = "captured"
			_ = a.pushSingleSnapshot(ctx, cmd.PrinterID, payload)
		} else {
			result["post_snapshot_error"] = snapErr.Error()
		}

		a.log.Info("command succeeded", "command_id", cmd.ID, "duration_ms", time.Since(start).Milliseconds())
		_ = a.cloud.CompleteCommand(ctx, cmd.ID, cloud.CommandCompleteRequest{
			Status: "succeeded",
			Result: result,
		})
	}

	return nil
}
