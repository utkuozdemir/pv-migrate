package ssh

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

const (
	rsaKeyAlgorithm     = "rsa"
	ed25519KeyAlgorithm = "ed25519"

	rsaKeyLengthBits = 2048
)

func CreateSSHKeyPair(keyAlgorithm string) (string, string, error) {
	switch keyAlgorithm {
	case rsaKeyAlgorithm:
		privateKey, err := rsa.GenerateKey(rand.Reader, rsaKeyLengthBits)
		if err != nil {
			return "", "", fmt.Errorf("failed to generate rsa key pair: %w", err)
		}

		return marshalKeyPair(&privateKey.PublicKey, privateKey)
	case ed25519KeyAlgorithm:
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return "", "", fmt.Errorf("failed to generate ed25519 key pair: %w", err)
		}

		return marshalKeyPair(publicKey, privateKey)
	default:
		return "", "", fmt.Errorf("unsupported key algorithm: %s", keyAlgorithm)
	}
}

func marshalKeyPair(publicKey crypto.PublicKey, privateKey crypto.PrivateKey) (string, string, error) {
	privateKeyPEM, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal private key: %w", err)
	}

	pub, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate public key: %w", err)
	}

	return string(ssh.MarshalAuthorizedKey(pub)), string(pem.EncodeToMemory(privateKeyPEM)), nil
}
