package bambu

import (
	"encoding/json"
	"testing"
)

func TestPrintControlRequestShape(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		command string
	}{
		{"pause", pauseRequest(1), "pause"},
		{"resume", resumeRequest(2), "resume"},
		{"stop", stopRequest(3), "stop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var env map[string]map[string]any
			if err := json.Unmarshal(tc.payload, &env); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			p, ok := env["print"]
			if !ok {
				t.Fatal("payload missing 'print' envelope")
			}
			if p["command"] != tc.command {
				t.Errorf("command = %v, want %q", p["command"], tc.command)
			}
			if p["param"] != "" {
				t.Errorf("param = %v, want empty string", p["param"])
			}
			// project_file-only fields must be omitted from control commands.
			if _, ok := p["url"]; ok {
				t.Error("control command unexpectedly carries a url field")
			}
		})
	}
}

func TestPushAllRequestShape(t *testing.T) {
	var env map[string]map[string]any
	if err := json.Unmarshal(pushAllRequest(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	pushing, ok := env["pushing"]
	if !ok {
		t.Fatal("payload missing 'pushing' envelope")
	}
	if pushing["command"] != "pushall" {
		t.Errorf("command = %v, want pushall", pushing["command"])
	}
}

func TestProjectFileRequestShape(t *testing.T) {
	var env map[string]map[string]any
	if err := json.Unmarshal(projectFileRequest(7, "Cool Model.3mf", "abc123def"), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	p := env["print"]
	if p["command"] != "project_file" {
		t.Errorf("command = %v, want project_file", p["command"])
	}
	if p["sequence_id"] != "7" {
		t.Errorf("sequence_id = %v, want 7", p["sequence_id"])
	}
	if p["url"] != "ftp:///Cool Model.3mf" {
		t.Errorf("url = %v, want ftp:///Cool Model.3mf", p["url"])
	}
	if p["md5"] != "abc123def" {
		t.Errorf("md5 = %v, want abc123def", p["md5"])
	}
	if p["subtask_name"] != "Cool Model" {
		t.Errorf("subtask_name = %v, want 'Cool Model' (extension stripped)", p["subtask_name"])
	}
	if p["use_ams"] != false {
		t.Errorf("use_ams = %v, want false", p["use_ams"])
	}
	if p["bed_type"] != "auto" {
		t.Errorf("bed_type = %v, want auto", p["bed_type"])
	}
}

func TestDeepMergePreservesSiblings(t *testing.T) {
	base := map[string]any{
		"print": map[string]any{
			"gcode_state":   "RUNNING",
			"nozzle_temper": 200.0,
			"bed_temper":    60.0,
		},
	}
	// A partial push that only updates the nozzle temperature.
	deepMerge(base, map[string]any{
		"print": map[string]any{"nozzle_temper": 210.0},
	})
	p := base["print"].(map[string]any)
	if p["nozzle_temper"] != 210.0 {
		t.Errorf("nozzle_temper = %v, want updated to 210", p["nozzle_temper"])
	}
	if p["bed_temper"] != 60.0 {
		t.Errorf("bed_temper = %v, want preserved at 60", p["bed_temper"])
	}
	if p["gcode_state"] != "RUNNING" {
		t.Errorf("gcode_state = %v, want preserved", p["gcode_state"])
	}
}

func TestCloneMapIsIndependent(t *testing.T) {
	src := map[string]any{"print": map[string]any{"mc_percent": 10.0}}
	clone := cloneMap(src)
	src["print"].(map[string]any)["mc_percent"] = 99.0
	if clone["print"].(map[string]any)["mc_percent"] != 10.0 {
		t.Error("clone should not observe mutations to the source")
	}
}

// The printer's own files live in subdirectories (/cache, /model), not at the
// FTP root, so a path returned by ListFiles must survive into the print URL.
// A bare name still resolves to the root, where ftpsUpload puts our uploads.
func TestProjectFileRequestPaths(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		wantURL     string
		wantSubtask string
	}{
		{"bare name resolves to root", "Benchy.3mf", "ftp:///Benchy.3mf", "Benchy"},
		{"cache path is preserved", "/cache/3DBenchy.3mf", "ftp:///cache/3DBenchy.3mf", "3DBenchy"},
		{"model path is preserved", "/model/Panda.gcode.3mf", "ftp:///model/Panda.gcode.3mf", "Panda.gcode"},
		{"relative path gains a leading slash", "cache/x.3mf", "ftp:///cache/x.3mf", "x"},
		{"name with spaces", "/cache/Speed Boat.3mf", "ftp:///cache/Speed Boat.3mf", "Speed Boat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var env map[string]map[string]any
			if err := json.Unmarshal(projectFileRequest(1, tt.filename, ""), &env); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			p := env["print"]
			if p["url"] != tt.wantURL {
				t.Errorf("url = %v, want %v", p["url"], tt.wantURL)
			}
			if p["subtask_name"] != tt.wantSubtask {
				t.Errorf("subtask_name = %v, want %v", p["subtask_name"], tt.wantSubtask)
			}
		})
	}
}
