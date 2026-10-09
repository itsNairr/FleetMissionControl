package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"time"
	"os"
	"encoding/pem"
	"crypto/x509"
	"path/filepath"
	"errors"

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

// SavePrivateKeyPEM writes an ECDSA private key to disk in PEM format (0600 permissions)
func SavePrivateKeyPEM(filePath string, privKey *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}
	block := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: der,
	}
	return os.WriteFile(filePath, pem.EncodeToMemory(block), 0600)
}

// SavePublicKeyPEM writes an ECDSA public key to disk in PEM format (0644 permissions)
func SavePublicKeyPEM(filePath string, pubKey *ecdsa.PublicKey) error {
	der, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		return fmt.Errorf("failed to marshal public key: %w", err)
	}
	block := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: der,
	}
	return os.WriteFile(filePath, pem.EncodeToMemory(block), 0644)
}

// LoadPrivateKeyPEM reads and decodes an ECDSA private key from a PEM file
func LoadPrivateKeyPEM(filePath string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "EC PRIVATE KEY" {
		return nil, fmt.Errorf("invalid private key PEM data in %s", filePath)
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

// LoadPublicKeyPEM reads and decodes an ECDSA public key from a PEM file
func LoadPublicKeyPEM(filePath string) (*ecdsa.PublicKey, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("invalid public key PEM data in %s", filePath)
	}
	parsedKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}
	pubKey, ok := parsedKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("key is not an ECDSA public key")
	}
	return pubKey, nil
}

// EnsureFleetKeys loads the fleet keypair from certsDir, or generates and saves them if they don't exist
func EnsureFleetKeys(certsDir string) (*ecdsa.PrivateKey, *ecdsa.PublicKey, error) {
	privPath := filepath.Join(certsDir, "fleet_private.pem")
	pubPath := filepath.Join(certsDir, "fleet_public.pem")

	// If both files exist, load them
	if _, err := os.Stat(privPath); err == nil {
		priv, err := LoadPrivateKeyPEM(privPath)
		if err == nil {
			pub, err := LoadPublicKeyPEM(pubPath)
			if err == nil {
				return priv, pub, nil
			}
		}
	}

	// Otherwise create directory and generate fresh keypair
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		return nil, nil, err
	}

	priv, pub, err := GenerateKeyPair()
	if err != nil {
		return nil, nil, err
	}

	if err := SavePrivateKeyPEM(privPath, priv); err != nil {
		return nil, nil, err
	}
	if err := SavePublicKeyPEM(pubPath, pub); err != nil {
		return nil, nil, err
	}

	fmt.Printf("🔑 Generated fresh fleet ECDSA keypair in %s\n", certsDir)
	return priv, pub, nil
}
