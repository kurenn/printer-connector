package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"printer-connector/internal/backup"
	"printer-connector/internal/cloud"
	"printer-connector/internal/driver"
	"printer-connector/internal/gcode"
	"printer-connector/internal/moonraker"
	"printer-connector/internal/util"
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

		mc := a.driverFor(cmd.PrinterID)
		if mc == nil {
			a.completeCommand(ctx, cmd.ID, cloud.CommandCompleteRequest{
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
		case "homing":
			// Optional axes parameter: {"axes": ["X", "Y"]} or empty for all
			var axes []string
			if axesParam, ok := cmd.Params["axes"].([]any); ok {
				for _, a := range axesParam {
					if axisStr, ok := a.(string); ok {
						axes = append(axes, axisStr)
					}
				}
			}
			if len(axes) > 0 {
				result["axes"] = axes
			} else {
				result["axes"] = "all"
			}
			execErr = mc.Home(ctx, axes...)
		case "upload_file":
			execErr = a.executeUploadFile(ctx, mc, cmd, result)
		case "delete_file":
			execErr = a.executeDeleteFile(ctx, mc, cmd, result)
		case "sync_files":
			execErr = a.executeSyncFiles(ctx, mc, cmd, result)
		case "import_history":
			execErr = a.executeImportHistory(ctx, mc, cmd, result)
		case "create_backup":
			execErr = a.executeCreateBackup(ctx, cmd, result)
		case "fetch_gcode":
			execErr = a.executeFetchGcode(ctx, cmd, result)
		default:
			execErr = fmt.Errorf("unsupported action: %s", cmd.Action)
		}

		if execErr != nil {
			a.log.Warn("command failed", "command_id", cmd.ID, "error", execErr)
			a.completeCommand(ctx, cmd.ID, cloud.CommandCompleteRequest{
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
		a.completeCommand(ctx, cmd.ID, cloud.CommandCompleteRequest{
			Status: "succeeded",
			Result: result,
		})
	}

	return nil
}

// completeCommand reports a command's outcome to the cloud, retrying a few
// times with backoff. The cloud marks commands "running" when it hands them
// out and only re-delivers "queued" ones, so a dropped completion would
// otherwise strand the command in "running" forever — after its side effect
// (delete, start_print, …) has already been applied locally. Retrying closes
// that window for transient failures.
func (a *Agent) completeCommand(ctx context.Context, id cloud.StringOrNumber, req cloud.CommandCompleteRequest) {
	const maxAttempts = 3
	bo := util.NewBackoff(500*time.Millisecond, 5*time.Second)

	for attempt := 1; ; attempt++ {
		if err := a.cloud.CompleteCommand(ctx, id, req); err == nil {
			return
		} else if attempt >= maxAttempts || ctx.Err() != nil {
			a.log.Error("could not report command completion; command may be stuck in running",
				"command_id", id, "status", req.Status, "attempts", attempt, "error", err)
			return
		} else {
			a.log.Warn("retrying command completion", "command_id", id, "attempt", attempt, "error", err)
		}

		if err := util.Wait(ctx, bo.Next()); err != nil {
			a.log.Error("aborting command completion retries", "command_id", id, "error", err)
			return
		}
	}
}

func (a *Agent) executeUploadFile(ctx context.Context, mc driver.Driver, cmd cloud.Command, result map[string]any) error {
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

func (a *Agent) executeDeleteFile(ctx context.Context, mc driver.Driver, cmd cloud.Command, result map[string]any) error {
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

func (a *Agent) executeSyncFiles(ctx context.Context, mc driver.Driver, cmd cloud.Command, result map[string]any) error {
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

func (a *Agent) executeImportHistory(ctx context.Context, mc driver.Driver, cmd cloud.Command, result map[string]any) error {
	// Get limit from params, default to 50
	limit := 50
	if limitParam, ok := cmd.Params["limit"].(float64); ok {
		limit = int(limitParam)
	}

	// Fetch history from Moonraker
	history, err := mc.GetHistory(ctx, limit)
	if err != nil {
		return fmt.Errorf("failed to fetch history from moonraker: %w", err)
	}

	result["history"] = history

	// Count jobs if available in response
	if historyResult, ok := history["result"].(map[string]any); ok {
		if jobs, ok := historyResult["jobs"].([]any); ok {
			result["count"] = len(jobs)
			a.log.Info("history imported", "command_id", cmd.ID, "count", len(jobs))
		}
	}

	return nil
}

func (a *Agent) executeCreateBackup(ctx context.Context, cmd cloud.Command, result map[string]any) error {
	// Extract and validate params
	backupID, _ := cmd.Params["backup_id"].(string)
	if backupID == "" {
		return fmt.Errorf("missing params.backup_id")
	}

	presignedURL, _ := cmd.Params["presigned_url"].(string)
	if presignedURL == "" {
		return fmt.Errorf("missing params.presigned_url")
	}

	// Backups pull the printer's files over Moonraker's file API, so the
	// connector does NOT need to run on the printer. Other driver types (Bambu)
	// don't expose this yet.
	mc, ok := a.driverFor(cmd.PrinterID).(*moonraker.Client)
	if !ok {
		return fmt.Errorf("remote backup is only supported for Moonraker printers (printer_id %d)", cmd.PrinterID)
	}

	// Parse include options (default all to false). include.database is accepted
	// but not fetched over Moonraker (the DB isn't a file-manager root).
	includeMap, _ := cmd.Params["include"].(map[string]any)
	includeConfig, _ := includeMap["config"].(bool)
	includeGcodes, _ := includeMap["gcodes"].(bool)
	includeLogs, _ := includeMap["logs"].(bool)

	if !includeConfig && !includeGcodes && !includeLogs {
		return fmt.Errorf("no fetchable directories selected for backup (config/gcodes/logs)")
	}

	// Stage + archive in the OS temp dir — transient, and writable for any user
	// (no dependency on a system state directory).
	outputPath := filepath.Join(os.TempDir(), "spoolr-backup-"+backupID+".tar.gz")

	a.log.Info("creating backup over moonraker",
		"backup_id", backupID,
		"printer_id", cmd.PrinterID,
		"include_config", includeConfig,
		"include_gcodes", includeGcodes,
		"include_logs", includeLogs,
	)

	backupResult, err := backup.FetchAndCreate(ctx, moonrakerFetcher{mc: mc}, backup.Options{
		IncludeConfig: includeConfig,
		IncludeGcodes: includeGcodes,
		IncludeLogs:   includeLogs,
		OutputPath:    outputPath,
		MaxSizeBytes:  10 << 30, // 10GB limit
	})
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Always cleanup temp archive after upload (or failure)
	defer func() {
		if err := os.Remove(backupResult.ArchivePath); err != nil {
			a.log.Warn("failed to cleanup backup archive", "path", backupResult.ArchivePath, "error", err)
		}
	}()

	a.log.Info("backup archive created",
		"backup_id", backupID,
		"size_bytes", backupResult.SizeBytes,
		"sha256", backupResult.SHA256,
	)

	// Upload to presigned URL
	if err := a.cloud.UploadBackup(ctx, presignedURL, backupResult.ArchivePath); err != nil {
		return fmt.Errorf("failed to upload backup: %w", err)
	}

	a.log.Info("backup uploaded successfully", "backup_id", backupID)

	// Populate result
	result["backup_id"] = backupID
	result["size_bytes"] = backupResult.SizeBytes
	result["sha256"] = backupResult.SHA256
	result["uploaded_at"] = time.Now().UTC().Format(time.RFC3339)

	return nil
}

// fetchGcodeTimeout bounds the whole fetch+upload of a single G-code file.
// Generous because the files can be tens of MB over a slow LAN, but finite so
// a stuck transfer can't pin a command in "running" forever.
const fetchGcodeTimeout = 5 * time.Minute

// executeFetchGcode pulls the currently-printing G-code from Moonraker and
// uploads it to the cloud's presigned URL so the web UI can render a live 3D
// toolpath. Mirrors executeCreateBackup: stream to a temp file (never buffer
// the whole file in memory) then PUT it back.
func (a *Agent) executeFetchGcode(ctx context.Context, cmd cloud.Command, result map[string]any) error {
	filename, _ := cmd.Params["filename"].(string)
	if filename == "" {
		return fmt.Errorf("missing params.filename for fetch_gcode")
	}
	uploadURL, _ := cmd.Params["upload_url"].(string)
	if uploadURL == "" {
		return fmt.Errorf("missing params.upload_url for fetch_gcode")
	}

	// G-code lives under Moonraker's "gcodes" file root, so this only works for
	// Moonraker printers (Bambu et al. don't expose that API yet).
	mc, ok := a.driverFor(cmd.PrinterID).(*moonraker.Client)
	if !ok {
		return fmt.Errorf("gcode fetch is only supported for Moonraker printers (printer_id %d)", cmd.PrinterID)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, fetchGcodeTimeout)
	defer cancel()

	a.log.Info("fetching active gcode over moonraker", "printer_id", cmd.PrinterID, "filename", filename)

	fetched, err := gcode.FetchToTemp(fetchCtx, mc, filename)
	if err != nil {
		return fmt.Errorf("failed to fetch gcode: %w", err)
	}
	defer func() {
		if rmErr := os.Remove(fetched.Path); rmErr != nil && !os.IsNotExist(rmErr) {
			a.log.Warn("failed to cleanup fetched gcode", "path", fetched.Path, "error", rmErr)
		}
	}()

	a.log.Info("gcode fetched", "printer_id", cmd.PrinterID, "size_bytes", fetched.SizeBytes)

	if err := a.cloud.UploadGcode(fetchCtx, uploadURL, fetched.Path); err != nil {
		return fmt.Errorf("failed to upload gcode: %w", err)
	}

	result["filename"] = filename
	result["size_bytes"] = fetched.SizeBytes
	result["uploaded_at"] = time.Now().UTC().Format(time.RFC3339)

	a.log.Info("gcode uploaded successfully", "printer_id", cmd.PrinterID, "filename", filename)
	return nil
}

// moonrakerFetcher adapts a *moonraker.Client to backup.Fetcher.
type moonrakerFetcher struct{ mc *moonraker.Client }

func (m moonrakerFetcher) ListFiles(ctx context.Context, root string) ([]backup.RemoteFile, error) {
	entries, err := m.mc.ListRootFiles(ctx, root)
	if err != nil {
		return nil, err
	}
	files := make([]backup.RemoteFile, len(entries))
	for i, e := range entries {
		files[i] = backup.RemoteFile{Path: e.Path, Size: e.Size}
	}
	return files, nil
}

func (m moonrakerFetcher) DownloadFile(ctx context.Context, root, path string, w io.Writer) error {
	return m.mc.DownloadFile(ctx, root, path, w)
}
