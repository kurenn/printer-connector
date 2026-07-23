package bambu

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// transport is the seam between the Client and a live MQTT session. The real
// implementation is *mqttConn; tests inject a fake so Client behavior (state
// gating, command publishing) is exercised without a broker.
type transport interface {
	// Publish sends payload to topic and waits briefly for the broker ack.
	Publish(ctx context.Context, topic string, qos byte, payload []byte) error
	// LatestReport returns a snapshot of the merged report, when it last
	// changed, and whether the MQTT session is currently connected.
	LatestReport() (report map[string]any, lastReport time.Time, connected bool)
	// Close tears down the session.
	Close() error
}

const (
	mqttPort        = "8883"
	mqttUser        = "bblp"
	connectTimeout  = 6 * time.Second
	publishTimeout  = 5 * time.Second
	mqttKeepAliveS  = 30
	subscribeQoS    = byte(0)
	disconnectGrace = 250 // ms
)

// mqttConn holds a paho session to one printer and the deep-merged report it
// accumulates from the device/{serial}/report topic.
type mqttConn struct {
	cli    mqtt.Client
	serial string

	mu         sync.RWMutex
	report     map[string]any
	lastReport time.Time
	connected  bool
}

var _ transport = (*mqttConn)(nil)

// dialMQTT opens a TLS MQTT session to a Bambu printer in LAN/developer mode.
// Bambu presents a self-signed certificate, so verification is skipped — the
// access code is the actual authentication. On connect (and every auto-reconnect)
// it subscribes to the report topic and requests a full state push.
func dialMQTT(ctx context.Context, host, serial, accessCode string) (*mqttConn, error) {
	m := &mqttConn{serial: serial, report: map[string]any{}}

	opts := mqtt.NewClientOptions()
	opts.AddBroker("ssl://" + net.JoinHostPort(host, mqttPort))
	opts.SetClientID("printer-connector-" + randomID())
	opts.SetUsername(mqttUser)
	opts.SetPassword(accessCode)
	opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: true}) // #nosec G402 — LAN, self-signed; access code authenticates
	opts.SetCleanSession(true)
	opts.SetOrderMatters(false)
	opts.SetKeepAlive(mqttKeepAliveS * time.Second)
	opts.SetConnectTimeout(connectTimeout)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(30 * time.Second)
	opts.SetOnConnectHandler(m.onConnect)
	opts.SetConnectionLostHandler(m.onConnectionLost)

	m.cli = mqtt.NewClient(opts)

	token := m.cli.Connect()
	if !waitToken(ctx, token, connectTimeout) {
		m.cli.Disconnect(0)
		return nil, errors.New("bambu mqtt: connect timed out")
	}
	if err := token.Error(); err != nil {
		m.cli.Disconnect(0)
		return nil, err
	}
	return m, nil
}

func (m *mqttConn) onConnect(cli mqtt.Client) {
	m.mu.Lock()
	m.connected = true
	m.mu.Unlock()

	cli.Subscribe("device/"+m.serial+"/report", subscribeQoS, m.onMessage)
	// Seed the merged report with a full snapshot instead of waiting for the
	// next incremental push, and ask which model this is — the state push alone
	// doesn't say, and normalization depends on it.
	cli.Publish("device/"+m.serial+"/request", subscribeQoS, false, pushAllRequest())
	cli.Publish("device/"+m.serial+"/request", subscribeQoS, false, getVersionRequest())
}

func (m *mqttConn) onConnectionLost(_ mqtt.Client, _ error) {
	m.mu.Lock()
	m.connected = false
	m.mu.Unlock()
}

// onMessage deep-merges each report into the accumulated state. Bambu sends
// partial diffs (only changed fields) between full pushes, so merging — not
// replacing — is required to keep a coherent view.
func (m *mqttConn) onMessage(_ mqtt.Client, msg mqtt.Message) {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		return
	}
	m.mu.Lock()
	deepMerge(m.report, payload)
	m.lastReport = time.Now()
	m.mu.Unlock()
}

func (m *mqttConn) Publish(ctx context.Context, topic string, qos byte, payload []byte) error {
	token := m.cli.Publish(topic, qos, false, payload)
	if !waitToken(ctx, token, publishTimeout) {
		return errors.New("bambu mqtt: publish timed out")
	}
	return token.Error()
}

func (m *mqttConn) LatestReport() (map[string]any, time.Time, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneMap(m.report), m.lastReport, m.connected
}

func (m *mqttConn) Close() error {
	m.cli.Disconnect(disconnectGrace)
	return nil
}

// waitToken blocks until the paho token completes, the context is cancelled, or
// the timeout elapses, whichever comes first.
func waitToken(ctx context.Context, token mqtt.Token, timeout time.Duration) bool {
	select {
	case <-token.Done():
		return true
	case <-ctx.Done():
		return false
	case <-time.After(timeout):
		return false
	}
}

func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(b)
}

// deepMerge recursively merges src into dst: nested maps merge key-by-key, while
// scalars and arrays replace. This matches how Bambu layers partial updates over
// a base state (e.g. a single changed temperature) without dropping siblings.
func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if sv, ok := v.(map[string]any); ok {
			if dv, ok := dst[k].(map[string]any); ok {
				deepMerge(dv, sv)
				continue
			}
			nm := map[string]any{}
			deepMerge(nm, sv)
			dst[k] = nm
			continue
		}
		dst[k] = v
	}
}

// cloneMap returns a deep copy so callers can read the report without racing the
// onMessage writer. A JSON round-trip keeps it simple and is cheap at the
// snapshot cadence (tens of seconds).
func cloneMap(m map[string]any) map[string]any {
	if len(m) == 0 {
		return map[string]any{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{}
	}
	return out
}
