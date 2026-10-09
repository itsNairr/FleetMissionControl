package command

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"time"

	"github.com/itsnairr/fleet-telemetry-engine/internal/crypto"
	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
	"github.com/itsnairr/fleet-telemetry-engine/internal/session"
	"google.golang.org/protobuf/proto"
)

// CommandDispatcher manages signing and transmitting authenticated commands to connected vehicles
type CommandDispatcher struct {
	privKey  *ecdsa.PrivateKey
	sessions *session.SessionRegistry
}

// NewCommandDispatcher creates a new dispatcher with the gateway private key and socket session registry
func NewCommandDispatcher(privKey *ecdsa.PrivateKey, sessions *session.SessionRegistry) *CommandDispatcher {
	return &CommandDispatcher{
		privKey:  privKey,
		sessions: sessions,
	}
}

// Dispatch signs and sends a vehicle command down the active TCP connection for the target VIN
func (d *CommandDispatcher) Dispatch(vin string, cmdType pb.CommandType, speedLimitMph float64) (*pb.VehicleCommand, error) {
	// Check if vehicle is currently connected
	conn, exists := d.sessions.Get(vin)
	if !exists {
		return nil, fmt.Errorf("vehicle %s is offline (no active socket session)", vin)
	}

	// Generate random cryptographic nonce to prevent replay attacks
	nonceBig, err := rand.Int(rand.Reader, big.NewInt(1000000000))
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Construct command message
	cmd := &pb.VehicleCommand{
		CommandId:     fmt.Sprintf("cmd-%d", time.Now().UnixNano()),
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

	// Serialize Protobuf
	payload, err := proto.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command protobuf: %w", err)
	}

	// Encode 4-byte length prefix (Big-Endian network standard)
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(payload)))

	// Write header + payload to vehicle socket
	if _, err := conn.Write(header); err != nil {
		return nil, fmt.Errorf("failed to write command header to %s: %w", vin, err)
	}
	if _, err := conn.Write(payload); err != nil {
		return nil, fmt.Errorf("failed to write command payload to %s: %w", vin, err)
	}

	fmt.Printf("[Dispatcher] 🚀 Dispatched %s to %s (ID: %s)\n", cmd.Type, vin, cmd.CommandId)
	return cmd, nil
}
