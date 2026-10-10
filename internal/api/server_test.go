package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/itsnairr/fleet-telemetry-engine/internal/command"
	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
	"github.com/itsnairr/fleet-telemetry-engine/internal/pubsub"
	"github.com/itsnairr/fleet-telemetry-engine/internal/twin"
)

// fakeSender stands in for the real dispatcher so tests don't need a TCP vehicle
type fakeSender struct {
	gotVin   string
	gotType  pb.CommandType
	gotSpeed float64
	resp     *pb.CommandResponse
	err      error
}

func (f *fakeSender) DispatchAndWait(ctx context.Context, vin string, cmdType pb.CommandType, speed float64) (*pb.CommandResponse, error) {
	f.gotVin, f.gotType, f.gotSpeed = vin, cmdType, speed
	return f.resp, f.err
}

type testEnv struct {
	server *httptest.Server
	twins  *twin.DigitalTwinRegistry
	alerts *pubsub.Broker[*pb.VehicleAlert]
	sender *fakeSender
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	twins := twin.NewDigitalTwinRegistry()
	alerts := pubsub.NewBroker[*pb.VehicleAlert](16)
	sender := &fakeSender{}

	s := NewServer(Config{AllowedOrigins: []string{"http://localhost:5173"}}, twins, sender, alerts)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)

	return &testEnv{server: server, twins: twins, alerts: alerts, sender: sender}
}

func (e *testEnv) addVehicle(vin string, online bool, speed float32) {
	e.twins.Update(&pb.VehicleTelemetry{Vin: vin, SpeedKmh: speed})
	e.twins.SetOnline(vin, online)
}

func (e *testEnv) postCommand(t *testing.T, vin, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(e.server.URL+"/api/v1/vehicles/"+vin+"/command", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestListVehiclesReturnsSortedSnapshots(t *testing.T) {
	env := newTestEnv(t)
	env.addVehicle("SIM-0002", false, 0)
	env.addVehicle("SIM-0001", true, 88)

	resp, err := http.Get(env.server.URL + "/api/v1/vehicles")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var vehicles []struct {
		Vin       string         `json:"vin"`
		Online    bool           `json:"online"`
		Telemetry map[string]any `json:"telemetry"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&vehicles); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}

	if len(vehicles) != 2 || vehicles[0].Vin != "SIM-0001" || vehicles[1].Vin != "SIM-0002" {
		t.Fatalf("expected [SIM-0001, SIM-0002], got %+v", vehicles)
	}
	if !vehicles[0].Online || vehicles[1].Online {
		t.Fatalf("online flags wrong: %+v", vehicles)
	}
	if vehicles[0].Telemetry["speedKmh"] != 88.0 {
		t.Fatalf("expected speedKmh=88, got %v", vehicles[0].Telemetry["speedKmh"])
	}
}

func TestGetUnknownVehicleIs404(t *testing.T) {
	env := newTestEnv(t)
	resp, err := http.Get(env.server.URL + "/api/v1/vehicles/NOPE")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestCommandAcceptedAndShortNames(t *testing.T) {
	env := newTestEnv(t)
	env.sender.resp = &pb.CommandResponse{CommandId: "cmd-1", Success: true}

	status, body := env.postCommand(t, "SIM-0001", `{"type": "open_frunk"}`)
	if status != http.StatusOK || body["success"] != true || body["command_id"] != "cmd-1" {
		t.Fatalf("got %d %v", status, body)
	}
	if env.sender.gotVin != "SIM-0001" || env.sender.gotType != pb.CommandType_COMMAND_TYPE_OPEN_FRUNK {
		t.Fatalf("dispatcher got vin=%s type=%v", env.sender.gotVin, env.sender.gotType)
	}
}

func TestCommandRejectedByVehicleIs200WithReason(t *testing.T) {
	env := newTestEnv(t)
	env.sender.resp = &pb.CommandResponse{CommandId: "cmd-2", Success: false, ErrorMessage: "vehicle in motion"}

	status, body := env.postCommand(t, "SIM-0001", `{"type": "COMMAND_TYPE_OPEN_FRUNK"}`)
	if status != http.StatusOK || body["success"] != false || body["error"] != "vehicle in motion" {
		t.Fatalf("got %d %v", status, body)
	}
}

func TestCommandErrorStatusCodes(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		senderErr  error
		wantStatus int
	}{
		{"not json", `nope`, nil, http.StatusBadRequest},
		{"unknown type", `{"type": "SELF_DESTRUCT"}`, nil, http.StatusBadRequest},
		{"unspecified type", `{"type": "UNSPECIFIED"}`, nil, http.StatusBadRequest},
		{"speed limit without speed", `{"type": "SET_SPEED_LIMIT"}`, nil, http.StatusBadRequest},
		{"offline", `{"type": "LOCK_DOORS"}`, fmt.Errorf("vehicle X: %w", command.ErrVehicleOffline), http.StatusServiceUnavailable},
		{"no ack", `{"type": "LOCK_DOORS"}`, fmt.Errorf("no ack: %w", context.DeadlineExceeded), http.StatusGatewayTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.sender.err = tc.senderErr
			if status, body := env.postCommand(t, "SIM-0001", tc.body); status != tc.wantStatus {
				t.Fatalf("expected %d, got %d %v", tc.wantStatus, status, body)
			}
		})
	}
}

func TestSpeedLimitIsForwarded(t *testing.T) {
	env := newTestEnv(t)
	env.sender.resp = &pb.CommandResponse{Success: true}

	env.postCommand(t, "SIM-0001", `{"type": "SET_SPEED_LIMIT", "speed_limit_mph": 65}`)
	if env.sender.gotSpeed != 65 {
		t.Fatalf("expected speed 65, got %v", env.sender.gotSpeed)
	}
}

func dial(t *testing.T, env *testEnv, path string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(env.server.URL, "http")+path, nil)
	if err != nil {
		t.Fatalf("dial %s failed: %v", path, err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func readJSON(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var out map[string]any
	json.Unmarshal(data, &out)
	return out
}

func TestVehicleStreamPushesOnlyThatVehicle(t *testing.T) {
	env := newTestEnv(t)
	env.addVehicle("SIM-0001", true, 10)
	env.addVehicle("SIM-0002", true, 20)

	conn := dial(t, env, "/api/v1/vehicles/SIM-0001/stream")

	// 1. Current state arrives immediately on connect
	if msg := readJSON(t, conn); msg["vin"] != "SIM-0001" {
		t.Fatalf("expected initial SIM-0001 snapshot, got %v", msg)
	}

	// 2. Another car's update must be filtered out, ours must be pushed
	env.twins.Update(&pb.VehicleTelemetry{Vin: "SIM-0002", SpeedKmh: 99})
	env.twins.Update(&pb.VehicleTelemetry{Vin: "SIM-0001", SpeedKmh: 55})

	msg := readJSON(t, conn)
	telemetry, _ := msg["telemetry"].(map[string]any)
	if msg["vin"] != "SIM-0001" || telemetry["speedKmh"] != 55.0 {
		t.Fatalf("expected pushed SIM-0001 @ 55 km/h, got %v", msg)
	}

	// 3. Disconnect is pushed as online=false
	env.twins.SetOnline("SIM-0001", false)
	if msg := readJSON(t, conn); msg["online"] != false {
		t.Fatalf("expected offline push, got %v", msg)
	}
}

func TestAlertStream(t *testing.T) {
	env := newTestEnv(t)
	conn := dial(t, env, "/api/v1/alerts/stream")

	env.alerts.Publish(&pb.VehicleAlert{Vin: "SIM-0007", Code: "TPMS_LowPressure", Level: pb.VehicleAlert_ALERT_LEVEL_WARNING})

	msg := readJSON(t, conn)
	if msg["vin"] != "SIM-0007" || msg["code"] != "TPMS_LowPressure" || msg["level"] != "ALERT_LEVEL_WARNING" {
		t.Fatalf("unexpected alert payload: %v", msg)
	}
}

func TestWebSocketRejectsForeignOrigin(t *testing.T) {
	env := newTestEnv(t)
	env.addVehicle("SIM-0001", true, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(env.server.URL, "http")+"/api/v1/vehicles/SIM-0001/stream",
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://evil.example"}}})
	if err == nil {
		t.Fatal("expected a foreign origin to be refused")
	}
}

func TestCORSPreflight(t *testing.T) {
	env := newTestEnv(t)
	req, _ := http.NewRequest(http.MethodOptions, env.server.URL+"/api/v1/vehicles/SIM-0001/command", nil)
	req.Header.Set("Origin", "http://localhost:5173")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("missing CORS header, got %v", resp.Header)
	}
}
