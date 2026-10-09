package crypto

import (
	"testing"
	"time"

	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
)

func TestECDSASigningAndVerification(t *testing.T) {
	// Generate Gateway Keypair
	privKey, pubKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate keypair: %v", err)
	}

	cmd := &pb.VehicleCommand{
		CommandId:   "cmd-uuid-001",
		Vin:         "SIM-0001",
		Type:        pb.CommandType_COMMAND_TYPE_UNLOCK_DOORS,
		TimestampMs: time.Now().UnixMilli(),
		Nonce:       42,
	}

	// Sign Command
	if err := SignCommand(privKey, cmd); err != nil {
		t.Fatalf("SignCommand failed: %v", err)
	}

	// Invariant 1: Fresh, valid command should pass
	valid, reason := VerifyCommand(pubKey, cmd, 5*time.Second)
	if !valid {
		t.Fatalf("Expected valid signature, failed with reason: %s", reason)
	}

	// Invariant 2: Replay Attack (stale timestamp > 5s)
	staleCmd := &pb.VehicleCommand{
		CommandId:   cmd.CommandId,
		Vin:         cmd.Vin,
		Type:        cmd.Type,
		TimestampMs: time.Now().Add(-10 * time.Second).UnixMilli(), // 10s old
		Nonce:       cmd.Nonce,
	}
	_ = SignCommand(privKey, staleCmd)
	valid, _ = VerifyCommand(pubKey, staleCmd, 5*time.Second)
	if valid {
		t.Errorf("Security flaw: stale command (replay attack) was accepted!")
	}

	// Invariant 3: Tamper Detection (packet modified in-flight)
	tamperedCmd := &pb.VehicleCommand{
		CommandId:   cmd.CommandId,
		Vin:         cmd.Vin,
		Type:        pb.CommandType_COMMAND_TYPE_LOCK_DOORS, // Attacker flipped action!
		TimestampMs: cmd.TimestampMs,
		Nonce:       cmd.Nonce,
		Signature:   cmd.Signature, // Keeps original signature
	}
	valid, _ = VerifyCommand(pubKey, tamperedCmd, 5*time.Second)
	if valid {
		t.Errorf("Security flaw: tampered payload was accepted!")
	}

	// Invariant 4: Rogue Signer (unauthorized private key)
	roguePrivKey, _, _ := GenerateKeyPair()
	rogueCmd := &pb.VehicleCommand{
		CommandId:   "cmd-uuid-002",
		Vin:         "SIM-0001",
		Type:        pb.CommandType_COMMAND_TYPE_OPEN_FRUNK,
		TimestampMs: time.Now().UnixMilli(),
		Nonce:       99,
	}
	_ = SignCommand(roguePrivKey, rogueCmd)
	valid, _ = VerifyCommand(pubKey, rogueCmd, 5*time.Second)
	if valid {
		t.Errorf("Security flaw: command signed by rogue key was accepted!")
	}
}
