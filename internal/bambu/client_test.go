package bambu

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"printer-connector/internal/driver"
)

// validCapabilities mirrors printer_capabilities.json's enum.
var validCapabilities = map[string]bool{
	"pause": true, "resume": true, "cancel": true, "start_print": true,
	"upload_file": true, "delete_file": true, "sync_files": true,
	"import_history": true, "create_backup": true, "homing": true,
	"remote_print": true, "webcam": true,
}

// fakeTransport stands in for a live MQTT session so Client logic is testable
// without a broker.
type fakeTransport struct {
	report    map[string]any
	connected bool
	pubErr    error

	topics   []string
	qos      []byte
	payloads [][]byte
}

func (f *fakeTransport) Publish(_ context.Context, topic string, qos byte, payload []byte) error {
	f.topics = append(f.topics, topic)
	f.qos = append(f.qos, qos)
	f.payloads = append(f.payloads, payload)
	return f.pubErr
}

func (f *fakeTransport) LatestReport() (map[string]any, time.Time, bool) {
	return f.report, time.Now(), f.connected
}

func (f *fakeTransport) Close() error { return nil }

func clientWith(tr transport, dialErr error) *Client {
	c := New("192.168.1.50", "01ABC", "12345678")
	c.dial = func(context.Context) (transport, error) {
		if dialErr != nil {
			return nil, dialErr
		}
		return tr, nil
	}
	return c
}

func decodePrint(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var env map[string]map[string]any
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return env["print"]
}

func TestCapabilitiesAreValidAndImplemented(t *testing.T) {
	caps := New("h", "s", "a").Capabilities()
	if len(caps) == 0 {
		t.Fatal("expected non-empty capabilities")
	}
	for _, c := range caps {
		if !validCapabilities[c] {
			t.Errorf("capability %q not in the canonical set", c)
		}
	}
	// Actions we explicitly do not support must not be advertised.
	for _, c := range caps {
		if c == "webcam" || c == "homing" || c == "import_history" {
			t.Errorf("capability %q is advertised but not implemented", c)
		}
	}
}

func TestControlCommandsPublishToRequestTopic(t *testing.T) {
	cases := []struct {
		name    string
		call    func(*Client) error
		command string
	}{
		{"pause", func(c *Client) error { return c.Pause(context.Background()) }, "pause"},
		{"resume", func(c *Client) error { return c.Resume(context.Background()) }, "resume"},
		{"cancel", func(c *Client) error { return c.Cancel(context.Background()) }, "stop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ft := &fakeTransport{connected: true}
			c := clientWith(ft, nil)
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if len(ft.payloads) != 1 {
				t.Fatalf("expected 1 publish, got %d", len(ft.payloads))
			}
			if ft.topics[0] != "device/01ABC/request" {
				t.Errorf("topic = %q, want device/01ABC/request", ft.topics[0])
			}
			// QoS 0: Bambu never PUBACKs QoS-1 control publishes, which made every
			// command return a phantom timeout even when honored (verified on an
			// A1 mini). See publishPrint.
			if ft.qos[0] != 0 {
				t.Errorf("qos = %d, want 0", ft.qos[0])
			}
			if got := decodePrint(t, ft.payloads[0])["command"]; got != tc.command {
				t.Errorf("command = %v, want %q", got, tc.command)
			}
		})
	}
}

func TestSequenceIDIncrementsPerCommand(t *testing.T) {
	ft := &fakeTransport{connected: true}
	c := clientWith(ft, nil)
	_ = c.Pause(context.Background())
	_ = c.Resume(context.Background())
	first := decodePrint(t, ft.payloads[0])["sequence_id"]
	second := decodePrint(t, ft.payloads[1])["sequence_id"]
	if first != "1" || second != "2" {
		t.Errorf("sequence_ids = %v, %v; want 1, 2", first, second)
	}
}

func TestStartPrintPublishesProjectFile(t *testing.T) {
	ft := &fakeTransport{connected: true}
	c := clientWith(ft, nil)
	// A directory-qualified path (as ListFiles returns) must reach the printer
	// intact — its files live under /cache and /model, not at the FTP root.
	if err := c.StartPrint(context.Background(), "models/benchy.3mf"); err != nil {
		t.Fatalf("StartPrint: %v", err)
	}
	p := decodePrint(t, ft.payloads[0])
	if p["command"] != "project_file" {
		t.Errorf("command = %v, want project_file", p["command"])
	}
	if p["url"] != "ftp:///models/benchy.3mf" {
		t.Errorf("url = %v, want ftp:///models/benchy.3mf", p["url"])
	}
	if p["subtask_name"] != "benchy" {
		t.Errorf("subtask_name = %v, want benchy", p["subtask_name"])
	}
	if p["param"] != "Metadata/plate_1.gcode" {
		t.Errorf("param = %v, want Metadata/plate_1.gcode", p["param"])
	}
}

func TestStartPrintRejectsEmptyFilename(t *testing.T) {
	c := clientWith(&fakeTransport{connected: true}, nil)
	if err := c.StartPrint(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty filename")
	}
}

func TestUploadFileDelegatesToFTPS(t *testing.T) {
	c := New("192.168.1.50", "01ABC", "12345678")
	var gotHost, gotCode, gotName string
	var gotContent []byte
	c.ftpUpload = func(host, code, name string, content []byte) error {
		gotHost, gotCode, gotName, gotContent = host, code, name, content
		return nil
	}
	if err := c.UploadFile(context.Background(), "benchy.3mf", []byte("3mf-bytes")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if gotHost != "192.168.1.50" || gotCode != "12345678" || gotName != "benchy.3mf" {
		t.Errorf("ftp args = %q/%q/%q", gotHost, gotCode, gotName)
	}
	if string(gotContent) != "3mf-bytes" {
		t.Errorf("content = %q", gotContent)
	}
}

func TestTelemetryReportsOfflineWhenUnreachable(t *testing.T) {
	t.Run("dial fails", func(t *testing.T) {
		c := clientWith(nil, errors.New("connection refused"))
		tel, err := c.Telemetry(context.Background())
		if err != nil {
			t.Fatalf("Telemetry should not error: %v", err)
		}
		if tel.State != driver.StateOffline {
			t.Errorf("state = %q, want offline", tel.State)
		}
	})
	t.Run("connected but no report", func(t *testing.T) {
		c := clientWith(&fakeTransport{connected: true, report: map[string]any{}}, nil)
		tel, _ := c.Telemetry(context.Background())
		if tel.State != driver.StateOffline {
			t.Errorf("state = %q, want offline", tel.State)
		}
	})
}

func TestTelemetryNormalizesLiveReport(t *testing.T) {
	ft := &fakeTransport{
		connected: true,
		report:    map[string]any{"print": map[string]any{"gcode_state": "RUNNING", "mc_percent": 50.0}},
	}
	c := clientWith(ft, nil)
	tel, err := c.Telemetry(context.Background())
	if err != nil {
		t.Fatalf("Telemetry: %v", err)
	}
	if tel.Driver != driver.Bambu {
		t.Errorf("driver = %q, want bambu", tel.Driver)
	}
	if tel.State != driver.StatePrinting {
		t.Errorf("state = %q, want printing", tel.State)
	}
}

func TestQueryObjectsErrorsWhenUnreachable(t *testing.T) {
	t.Run("not connected", func(t *testing.T) {
		c := clientWith(&fakeTransport{connected: false, report: map[string]any{"print": map[string]any{}}}, nil)
		if _, err := c.QueryObjects(context.Background()); err == nil {
			t.Fatal("expected error when disconnected so the snapshot loop skips it")
		}
	})
	t.Run("no telemetry yet", func(t *testing.T) {
		c := clientWith(&fakeTransport{connected: true, report: map[string]any{}}, nil)
		if _, err := c.QueryObjects(context.Background()); err == nil {
			t.Fatal("expected error when no report received yet")
		}
	})
}

// QueryObjects returns the raw merged report; the agent attaches "normalized"
// via NormalizeRaw, so QueryObjects must round-trip cleanly through it.
func TestQueryObjectsReturnsRawReport(t *testing.T) {
	report := map[string]any{"print": map[string]any{"gcode_state": "RUNNING", "mc_percent": 25.0}}
	c := clientWith(&fakeTransport{connected: true, report: report}, nil)

	raw, err := c.QueryObjects(context.Background())
	if err != nil {
		t.Fatalf("QueryObjects: %v", err)
	}
	if _, ok := raw["print"]; !ok {
		t.Fatal("expected raw report to carry the top-level 'print' object")
	}

	tel := c.NormalizeRaw(raw)
	if tel.Driver != driver.Bambu {
		t.Errorf("NormalizeRaw driver = %q, want bambu", tel.Driver)
	}
	if tel.State != driver.StatePrinting {
		t.Errorf("NormalizeRaw state = %q, want printing", tel.State)
	}
}

func TestNormalizeRawOfflineOnEmpty(t *testing.T) {
	tel := New("h", "s", "a").NormalizeRaw(map[string]any{})
	if tel.State != driver.StateOffline || tel.Driver != driver.Bambu {
		t.Errorf("empty NormalizeRaw = %+v, want offline bambu", tel)
	}
}

func TestUnsupportedActions(t *testing.T) {
	c := New("h", "s", "a")
	if err := c.Home(context.Background()); !errors.Is(err, ErrNotSupported) {
		t.Errorf("Home err = %v, want ErrNotSupported", err)
	}
	if _, err := c.GetHistory(context.Background(), 10); !errors.Is(err, ErrNotSupported) {
		t.Errorf("GetHistory err = %v, want ErrNotSupported", err)
	}
	if _, _, err := c.GetWebcamSnapshot(context.Background()); !errors.Is(err, ErrNotSupported) {
		t.Errorf("GetWebcamSnapshot err = %v, want ErrNotSupported", err)
	}
}
