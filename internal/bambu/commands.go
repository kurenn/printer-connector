package bambu

import (
	"encoding/json"
	"path"
	"strings"
)

// The Bambu LAN MQTT protocol wraps every command in a category object ("print"
// or "pushing") whose `command` field names the action. `sequence_id` echoes
// back on the matching report so a client can correlate; we increment it per
// command but don't currently block on the echo. These builders are pure so the
// exact wire shape is unit-tested without a broker.

type printCommand struct {
	SequenceID string `json:"sequence_id"`
	Command    string `json:"command"`
	Param      string `json:"param"`

	// project_file-only fields. omitempty keeps pause/resume/stop payloads
	// minimal and identical to what Bambu Studio emits.
	URL          string `json:"url,omitempty"`
	SubtaskName  string `json:"subtask_name,omitempty"`
	UseAMS       *bool  `json:"use_ams,omitempty"`
	Timelapse    *bool  `json:"timelapse,omitempty"`
	FlowCali     *bool  `json:"flow_cali,omitempty"`
	BedLeveling  *bool  `json:"bed_leveling,omitempty"`
	LayerInspect *bool  `json:"layer_inspect,omitempty"`
	VibrationCal *bool  `json:"vibration_cali,omitempty"`
	BedType      string `json:"bed_type,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`
	SubtaskID    string `json:"subtask_id,omitempty"`
	TaskID       string `json:"task_id,omitempty"`
	MD5          string `json:"md5,omitempty"`
}

func pauseRequest(seq int64) []byte  { return printRequest(seq, "pause") }
func resumeRequest(seq int64) []byte { return printRequest(seq, "resume") }
func stopRequest(seq int64) []byte   { return printRequest(seq, "stop") }

func printRequest(seq int64, command string) []byte {
	b, _ := json.Marshal(map[string]printCommand{
		"print": {SequenceID: itoa(seq), Command: command, Param: ""},
	})
	return b
}

// pushAllRequest asks the printer to publish its complete state. It is sent on
// every (re)connect so the merged report is seeded with a full snapshot rather
// than waiting for the next incremental push.
func pushAllRequest() []byte {
	b, _ := json.Marshal(map[string]map[string]any{
		"pushing": {
			"sequence_id": "0",
			"command":     "pushall",
			"version":     1,
			"push_target": 1,
		},
	})
	return b
}

// projectFileRequest builds the command that prints a 3MF already present on
// the printer. `param` selects the plate's sliced gcode inside the archive;
// plate 1 is the default a single-plate slice produces. AMS is left off for
// v1 — AMS slot mapping is a follow-up.
//
// filename may be a bare name (a file ftpsUpload placed at the root) or a
// directory-qualified path as returned by ListFiles (e.g. /cache/x.3mf), since
// the printer's own files live in subdirectories rather than the root.
func projectFileRequest(seq int64, filename string) []byte {
	abs := printerPath(filename)
	name := path.Base(abs)
	subtask := strings.TrimSuffix(name, path.Ext(name))
	no := false
	yes := true
	cmd := printCommand{
		SequenceID: itoa(seq),
		Command:    "project_file",
		Param:      "Metadata/plate_1.gcode",
		// "ftp://" + an absolute path yields the triple-slash form Bambu
		// Studio emits for root files ("ftp:///x.3mf") and addresses
		// subdirectories correctly ("ftp:///cache/x.3mf").
		URL:          "ftp://" + abs,
		SubtaskName:  subtask,
		UseAMS:       &no,
		Timelapse:    &no,
		FlowCali:     &no,
		BedLeveling:  &yes,
		LayerInspect: &no,
		VibrationCal: &yes,
		BedType:      "auto",
		ProfileID:    "0",
		ProjectID:    "0",
		SubtaskID:    "0",
		TaskID:       "0",
		MD5:          "",
	}
	b, _ := json.Marshal(map[string]printCommand{"print": cmd})
	return b
}

// printerPath normalizes a file reference to an absolute path on the printer's
// storage. Bare names resolve to the root (where ftpsUpload puts them), keeping
// the previous behaviour intact, while listed paths keep their directory.
func printerPath(filename string) string {
	f := strings.TrimSpace(filename)
	if f == "" {
		return "/"
	}
	return path.Clean("/" + f)
}
