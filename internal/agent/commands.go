package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"printer-connector/internal/cloud"
	"printer-connector/internal/moonraker"
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
		case "upload_file":
			execErr = a.executeUploadFile(ctx, mc, cmd, result)
		case "delete_file":
			execErr = a.executeDeleteFile(ctx, mc, cmd, result)
		case "sync_files":
			execErr = a.executeSyncFiles(ctx, mc, cmd, result)
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

func (a *Agent) executeUploadFile(ctx context.Context, mc *moonraker.Client, cmd cloud.Command, result map[string]any) error {
	filename, _ := cmd.Params["filename"].(string)
	if filename == "" {
		return fmt.Errorf("missing params.filename for upload_file")
	}

	contentBase64, _ := cmd.Params["content"].(string)
	if contentBase64 == "" {
		return fmt.Errorf("missing params.content for upload_file")
	}

	// Decode base64 content
	content, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		return fmt.Errorf("failed to decode base64 content: %w", err)
	}

	result["filename"] = filename
	result["size"] = len(content)

	// Upload to Moonraker
	if err := mc.UploadFile(ctx, filename, content); err != nil {
		return fmt.Errorf("failed to upload file to moonraker: %w", err)
	}

	a.log.Info("file uploaded", "command_id", cmd.ID, "filename", filename, "size", len(content))
	return nil
}

func (a *Agent) executeDeleteFile(ctx context.Context, mc *moonraker.Client, cmd cloud.Command, result map[string]any) error {
	filename, _ := cmd.Params["filename"].(string)
	if filename == "" {
		return fmt.Errorf("missing params.filename for delete_file")
	}

	result["filename"] = filename

	// Delete from Moonraker
	if err := mc.DeleteFile(ctx, filename); err != nil {
		return fmt.Errorf("failed to delete file from moonraker: %w", err)
	}

	a.log.Info("file deleted", "command_id", cmd.ID, "filename", filename)
	return nil
}

func (a *Agent) executeSyncFiles(ctx context.Context, mc *moonraker.Client, cmd cloud.Command, result map[string]any) error {
	// Fetch files list from Moonraker
	files, err := mc.ListFiles(ctx)
	if err != nil {
		return fmt.Errorf("failed to list files from moonraker: %w", err)
	}

	result["files"] = files
	result["count"] = len(files)

	a.log.Info("files synced", "command_id", cmd.ID, "count", len(files))
	return nil
}
