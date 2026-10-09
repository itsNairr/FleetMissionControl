package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"time"

	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
)

// GenerateKeyPair generates a NIST P-256 ECDSA keypair
func GenerateKeyPair() (*ecdsa.PrivateKey, *ecdsa.PublicKey, error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate ecdsa key: %w", err)
	}
	return privKey, &privKey.PublicKey, nil
}

// CanonicalPayload returns a deterministic byte representation of command data (excluding signature)
func CanonicalPayload(cmd *pb.VehicleCommand) []byte {
	return []byte(fmt.Sprintf("%s:%s:%d:%.2f:%d:%d",
		cmd.GetCommandId(),
		cmd.GetVin(),
		cmd.GetType(),
		cmd.GetSpeedLimitMph(),
		cmd.GetTimestampMs(),
		cmd.GetNonce(),
	))
}

// SignCommand signs the canonical payload of the command using the gateway's private key
func SignCommand(privKey *ecdsa.PrivateKey, cmd *pb.VehicleCommand) error {
	payload := CanonicalPayload(cmd)
	hash := sha256.Sum256(payload)

	sig, err := ecdsa.SignASN1(rand.Reader, privKey, hash[:])
	if err != nil {
		return fmt.Errorf("failed to sign command: %w", err)
	}

	cmd.Signature = sig
	return nil
}

// VerifyCommand validates command timestamp freshness and verifies the ECDSA signature
func VerifyCommand(pubKey *ecdsa.PublicKey, cmd *pb.VehicleCommand, maxAge time.Duration) (bool, string) {
	if pubKey == nil {
		return false, "missing public key"
	}
	if len(cmd.GetSignature()) == 0 {
		return false, "missing signature"
	}

	// Replay attack guardrail: Check timestamp freshness
	nowMs := time.Now().UnixMilli()
	diff := time.Duration(nowMs-cmd.GetTimestampMs()) * time.Millisecond
	if diff < -maxAge || diff > maxAge {
		return false, fmt.Sprintf("timestamp expired or skewed (drift: %v, max: %v)", diff, maxAge)
	}

	// Hash canonical payload
	payload := CanonicalPayload(cmd)
	hash := sha256.Sum256(payload)

	// Verify ASN.1 ECDSA signature
	valid := ecdsa.VerifyASN1(pubKey, hash[:], cmd.GetSignature())
	if !valid {
		return false, "cryptographic signature verification failed"
	}

	return true, ""
}
