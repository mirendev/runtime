package cloudauth

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
)

// KeyPair represents an ED25519 key pair for cluster authentication
type KeyPair struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
}

// GenerateKeyPair generates a new ED25519 key pair
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	return &KeyPair{
		PrivateKey: priv,
		PublicKey:  pub,
	}, nil
}

// PublicKeyPEM returns the public key in PEM format
func (kp *KeyPair) PublicKeyPEM() (string, error) {
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(kp.PublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to marshal public key: %w", err)
	}

	block := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	}

	return string(pem.EncodeToMemory(block)), nil
}

// PrivateKeyPEM returns the private key in PEM format
func (kp *KeyPair) PrivateKeyPEM() (string, error) {
	privKeyBytes, err := x509.MarshalPKCS8PrivateKey(kp.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to marshal private key: %w", err)
	}

	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privKeyBytes,
	}

	return string(pem.EncodeToMemory(block)), nil
}

// Fingerprint returns the SHA256 fingerprint of the public key
// in the format "SHA256:base64encoded" to match the server
func (kp *KeyPair) Fingerprint() string {
	hash := sha256.Sum256(kp.PublicKey)
	return "SHA256:" + strings.TrimSuffix(base64.StdEncoding.EncodeToString(hash[:]), "=")
}

// Sign signs a message with the private key
func (kp *KeyPair) Sign(message []byte) (string, error) {
	signature := ed25519.Sign(kp.PrivateKey, message)
	return base64.StdEncoding.EncodeToString(signature), nil
}

// LoadKeyPairFromPEM loads a key pair from PEM encoded strings
func LoadKeyPairFromPEM(privateKeyPEM string) (*KeyPair, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	// Try to parse as PKCS8 first (standard format)
	privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Try ED25519 seed format as fallback
		if len(block.Bytes) == ed25519.SeedSize {
			privKey = ed25519.NewKeyFromSeed(block.Bytes)
		} else {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
	}

	ed25519Key, ok := privKey.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not ED25519")
	}

	return &KeyPair{
		PrivateKey: ed25519Key,
		PublicKey:  ed25519Key.Public().(ed25519.PublicKey),
	}, nil
}

// VerifySignature verifies a signature against the public key
func VerifySignature(publicKey ed25519.PublicKey, message []byte, signatureBase64 string) bool {
	signature, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return false
	}

	return ed25519.Verify(publicKey, message, signature)
}

// PublicKeyFromPEM parses a public key from PEM format
func PublicKeyFromPEM(pemData string) (crypto.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	return pubKey, nil
}
