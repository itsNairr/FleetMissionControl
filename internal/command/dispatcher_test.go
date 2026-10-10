package command

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/itsnairr/fleet-telemetry-engine/internal/crypto"
	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
	"github.com/itsnairr/fleet-telemetry-engine/internal/session"
	"github.com/itsnairr/fleet-telemetry-engine/internal/simulator"
	"github.com/itsnairr/fleet-telemetry-engine/internal/wire"
	"google.golang.org/protobuf/proto"
)

// startFakeLink wires a dispatcher to a simulated vehicle over an in-memory socket (net.Pipe),
// mimicking the gateway's read loop on one end and the simulator's command listener on the other.
// ackAs is the VIN the gateway believes the socket belongs to.
func startFakeLink(t *testing.T, scenario simulator.Scenario, ackAs string) (*CommandDispatcher, string) {
	t.Helper()
	const vin = "SIM-TEST"

	privKey, pubKey, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}

	gatewaySide, vehicleSide := net.Pipe()
	t.Cleanup(func() { gatewaySide.Close(); vehicleSide.Close() })

	sessions := session.NewSessionRegistry()
	sessions.Register(vin, gatewaySide)
	d := NewCommandDispatcher(privKey, sessions)

	// Vehicle: verify + execute each command, then ack
	vehicle := simulator.NewSimulatedVehicle(vin, pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T, scenario)
	vehicleWriter := wire.NewFrameWriter(vehicleSide)
	go func() {
		for {
			_, payload, err := wire.ReadFrame(vehicleSide)
			if err != nil {
				return
			}
			cmd := &pb.VehicleCommand{}
			proto.Unmarshal(payload, cmd)

			resp := &pb.CommandResponse{CommandId: cmd.GetCommandId(), Vin: vin, Success: true}
			if err := vehicle.ExecuteProtoCommand(cmd, pubKey); err != nil {
				resp.Success = false
				resp.ErrorMessage = err.Error()
			}
			vehicleWriter.WriteMessage(wire.MsgCommandResponse, resp)
		}
	}()

	// Gateway: route acks back to the dispatcher
	go func() {
		for {
			_, payload, err := wire.ReadFrame(gatewaySide)
			if err != nil {
				return
			}
			resp := &pb.CommandResponse{}
			proto.Unmarshal(payload, resp)
			d.HandleResponse(ackAs, resp)
		}
	}()

	return d, vin
}

func TestDispatchAndWaitReturnsVehicleVerdict(t *testing.T) {
	// Highway scenario = moving at 110 km/h, so the frunk interlock must reject
	d, vin := startFakeLink(t, simulator.ScenarioHighwayCruising, "SIM-TEST")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := d.DispatchAndWait(ctx, vin, pb.CommandType_COMMAND_TYPE_OPEN_FRUNK, 0)
	if err != nil {
		t.Fatalf("expected an ack, got error: %v", err)
	}
	if resp.GetSuccess() {
		t.Fatal("expected OPEN_FRUNK to be rejected while in motion")
	}

	resp, err = d.DispatchAndWait(ctx, vin, pb.CommandType_COMMAND_TYPE_UNLOCK_DOORS, 0)
	if err != nil || !resp.GetSuccess() {
		t.Fatalf("expected UNLOCK_DOORS to be accepted, got resp=%v err=%v", resp, err)
	}
}

func TestConcurrentCommandsToSameVehicle(t *testing.T) {
	d, vin := startFakeLink(t, simulator.ScenarioParkedSentry, "SIM-TEST")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := d.DispatchAndWait(ctx, vin, pb.CommandType_COMMAND_TYPE_LOCK_DOORS, 0); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent dispatch failed: %v", err)
	}
}

func TestAckFromWrongVehicleIsIgnored(t *testing.T) {
	// The ack arrives on a socket the gateway attributes to a different car
	d, vin := startFakeLink(t, simulator.ScenarioParkedSentry, "SIM-IMPOSTER")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if _, err := d.DispatchAndWait(ctx, vin, pb.CommandType_COMMAND_TYPE_UNLOCK_DOORS, 0); err == nil {
		t.Fatal("expected timeout: an ack from another vehicle's socket must not resolve the command")
	}
}

func TestOfflineVehicleFailsFast(t *testing.T) {
	privKey, _, _ := crypto.GenerateKeyPair()
	d := NewCommandDispatcher(privKey, session.NewSessionRegistry())

	if _, err := d.DispatchAndWait(context.Background(), "SIM-GHOST", pb.CommandType_COMMAND_TYPE_LOCK_DOORS, 0); err == nil {
		t.Fatal("expected offline error")
	}
}
