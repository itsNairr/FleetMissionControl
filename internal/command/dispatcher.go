package command

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/itsnairr/fleet-telemetry-engine/internal/crypto"
	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
	"github.com/itsnairr/fleet-telemetry-engine/internal/session"
	"github.com/itsnairr/fleet-telemetry-engine/internal/wire"
)

// pendingCommand is a command waiting for the vehicle to acknowledge it
type pendingCommand struct {
	vin      string
	response chan *pb.CommandResponse
}

// ErrVehicleOffline is returned when the target VIN has no live socket (check with errors.Is)
var ErrVehicleOffline = errors.New("vehicle is offline (no active socket session)")

// CommandDispatcher manages signing and transmitting authenticated commands to connected vehicles
type CommandDispatcher struct {
	privKey  *ecdsa.PrivateKey
	sessions *session.SessionRegistry
	Quiet    bool // If true, suppresses stdout logging (useful in high-frequency benchmarks)

	mu      sync.Mutex
	pending map[string]pendingCommand // command_id -> waiting caller
}

// NewCommandDispatcher creates a new dispatcher with the gateway private key and socket session registry
func NewCommandDispatcher(privKey *ecdsa.PrivateKey, sessions *session.SessionRegistry) *CommandDispatcher {
	return &CommandDispatcher{
		privKey:  privKey,
		sessions: sessions,
		pending:  make(map[string]pendingCommand),
	}
}

// Dispatch signs and sends a vehicle command down the active TCP connection for the target VIN.
// Fire-and-forget: it returns once the bytes are written, without waiting for the vehicle's verdict.
func (d *CommandDispatcher) Dispatch(vin string, cmdType pb.CommandType, speedLimitMph float64) (*pb.VehicleCommand, error) {
	cmd, err := d.buildSignedCommand(vin, cmdType, speedLimitMph)
	if err != nil {
		return nil, err
	}
	if err := d.send(cmd); err != nil {
		return nil, err
	}
	return cmd, nil
}

// DispatchAndWait sends a command and blocks until the vehicle accepts/rejects it or ctx expires.
// This is what the operator-facing API uses so the UI can show "Rejected: vehicle in motion".
func (d *CommandDispatcher) DispatchAndWait(ctx context.Context, vin string, cmdType pb.CommandType, speedLimitMph float64) (*pb.CommandResponse, error) {
	cmd, err := d.buildSignedCommand(vin, cmdType, speedLimitMph)
	if err != nil {
		return nil, err
	}

	// Register BEFORE sending so a very fast reply can't arrive before anyone is listening
	responseCh := make(chan *pb.CommandResponse, 1)
	d.mu.Lock()
	d.pending[cmd.GetCommandId()] = pendingCommand{vin: vin, response: responseCh}
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.pending, cmd.GetCommandId())
		d.mu.Unlock()
	}()

	if err := d.send(cmd); err != nil {
		return nil, err
	}

	select {
	case resp := <-responseCh:
		return resp, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("vehicle %s did not acknowledge %s (%s): %w", vin, cmd.GetType(), cmd.GetCommandId(), ctx.Err())
	}
}

// HandleResponse routes a CommandResponse read off vin's socket to whoever is waiting on it.
// vin is the identity of the socket it arrived on, so one car can't answer for another car's command.
func (d *CommandDispatcher) HandleResponse(vin string, resp *pb.CommandResponse) {
	status := "✅ accepted"
	if !resp.GetSuccess() {
		status = "❌ rejected: " + resp.GetErrorMessage()
	}
	if !d.Quiet {
		fmt.Printf("[Dispatcher] %s %s %s\n", vin, resp.GetCommandId(), status)
	}

	d.mu.Lock()
	p, ok := d.pending[resp.GetCommandId()]
	d.mu.Unlock()

	if !ok || p.vin != vin {
		return // Fire-and-forget command, timed-out caller, or a spoofed ID from another vehicle
	}

	select {
	case p.response <- resp:
	default: // Duplicate ack, the first one already won
	}
}

func (d *CommandDispatcher) buildSignedCommand(vin string, cmdType pb.CommandType, speedLimitMph float64) (*pb.VehicleCommand, error) {
	// Generate random cryptographic nonce to prevent replay attacks
	nonceBig, err := rand.Int(rand.Reader, big.NewInt(1000000000))
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Random 128-bit command ID (like a UUID). Timestamps aren't unique enough: on Windows the clock is
	// coarse, so commands sent in the same instant would share an ID and their acks would get crossed.
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("failed to generate command id: %w", err)
	}

	// Construct command message
	cmd := &pb.VehicleCommand{
		CommandId:     "cmd-" + hex.EncodeToString(idBytes),
		Vin:           vin,
		Type:          cmdType,
		SpeedLimitMph: speedLimitMph,
		TimestampMs:   time.Now().UnixMilli(),
		Nonce:         nonceBig.Int64(),
	}

	// Digitally sign the canonical payload using Gateway Private Key
	if err := crypto.SignCommand(d.privKey, cmd); err != nil {
		return nil, fmt.Errorf("failed to sign command: %w", err)
	}
	return cmd, nil
}

func (d *CommandDispatcher) send(cmd *pb.VehicleCommand) error {
	// Check if vehicle is currently connected
	s, exists := d.sessions.Get(cmd.GetVin())
	if !exists {
		return fmt.Errorf("vehicle %s: %w", cmd.GetVin(), ErrVehicleOffline)
	}

	// Single locked write of the whole frame, safe even if many requests target the same car at once
	if err := s.Writer.WriteMessage(wire.MsgCommand, cmd); err != nil {
		return fmt.Errorf("failed to write command to %s: %w", cmd.GetVin(), err)
	}

	if !d.Quiet {
		fmt.Printf("[Dispatcher] 🚀 Dispatched %s to %s (ID: %s)\n", cmd.GetType(), cmd.GetVin(), cmd.GetCommandId())
	}
	return nil
}
