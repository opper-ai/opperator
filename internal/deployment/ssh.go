package deployment

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// generateSSHKeyPair generates a new ED25519 SSH key pair
// Returns (publicKey, privateKey, error)
func generateSSHKeyPair() (string, string, error) {
	// Generate ED25519 key pair (much smaller than RSA, more secure)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate ED25519 key: %w", err)
	}

	// Convert private key to OpenSSH format
	sshPrivateKey, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal private key: %w", err)
	}
	privateKeyBytes := pem.EncodeToMemory(sshPrivateKey)

	// Generate public key in OpenSSH format
	sshPublicKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create public key: %w", err)
	}
	publicKeyBytes := ssh.MarshalAuthorizedKey(sshPublicKey)

	return string(publicKeyBytes), string(privateKeyBytes), nil
}
