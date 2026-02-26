package ssh

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"

	"golang.org/x/crypto/ed25519"
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
		return createSSHRSAKeyPair()
	case ed25519KeyAlgorithm:
		return createSSHEd25519KeyPair()
	default:
		return "", "", fmt.Errorf("unsupported key algorithm: %s", keyAlgorithm)
	}
}

func createSSHRSAKeyPair() (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, rsaKeyLengthBits)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate rsa key pair: %w", err)
	}

	// generate and write private key as PEM
	var privKeyBuf strings.Builder

	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	if err := pem.Encode(&privKeyBuf, privateKeyPEM); err != nil {
		return "", "", fmt.Errorf("failed to encode private key: %w", err)
	}

	// generate and write public key
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate public key: %w", err)
	}

	var pubKeyBuf strings.Builder

	pubKeyBuf.Write(ssh.MarshalAuthorizedKey(pub))

	return pubKeyBuf.String(), privKeyBuf.String(), nil
}

func createSSHEd25519KeyPair() (string, string, error) {
	pubKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate ed25519 key pair: %w", err)
	}

	// generate and write private key as PEM
	var privKeyBuf strings.Builder

	ed25519PrivateKey, err := marshalED25519PrivateKey(privateKey)
	if err != nil {
		return "", "", err
	}

	privateKeyPEM := &pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: ed25519PrivateKey}
	if err := pem.Encode(&privKeyBuf, privateKeyPEM); err != nil {
		return "", "", fmt.Errorf("failed to encode private key: %w", err)
	}

	pub, _ := ssh.NewPublicKey(pubKey)

	var pubKeyBuf strings.Builder

	pubKeyBuf.Write(ssh.MarshalAuthorizedKey(pub))

	return pubKeyBuf.String(), privKeyBuf.String(), nil
}

// marshalED25519PrivateKey is taken from https://github.com/mikesmitty/edkey
func marshalED25519PrivateKey(key ed25519.PrivateKey) ([]byte, error) {
	magic := append([]byte("openssh-key-v1"), 0)

	message := ed25519message{}
	pk1 := ed25519pk1{}

	rnd, err := rand.Int(rand.Reader, big.NewInt(math.MaxUint32))
	if err != nil {
		return nil, fmt.Errorf("failed to generate random number: %w", err)
	}

	//nolint:gosec // it won't overflow, as the max value is set to math.MaxUint32
	{
		pk1.Check1 = uint32(rnd.Uint64())
		pk1.Check2 = uint32(rnd.Uint64())
	}

	pk1.Keytype = ssh.KeyAlgoED25519

	publicKey, ok := key.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("failed to convert public key")
	}

	pubKey := []byte(publicKey)

	pk1.Pub = pubKey
	pk1.Priv = key
	pk1.Comment = ""

	bs := 8
	blockLen := len(ssh.Marshal(pk1))
	padLen := (bs - (blockLen % bs)) % bs
	pk1.Pad = make([]byte, padLen)

	for i := range padLen {
		pk1.Pad[i] = byte(i + 1)
	}

	pubkeyFull := make([]byte, 0, 4+len(ssh.KeyAlgoED25519)+4+len(pubKey))
	pubkeyFull = append(pubkeyFull, 0x0, 0x0, 0x0, 0x0b) //nolint:mnd
	pubkeyFull = append(pubkeyFull, []byte(ssh.KeyAlgoED25519)...)
	pubkeyFull = append(pubkeyFull, []byte{0x0, 0x0, 0x0, 0x20}...)
	pubkeyFull = append(pubkeyFull, pubKey...)

	message.CipherName = "none"
	message.KdfName = "none"
	message.KdfOpts = ""
	message.NumKeys = 1
	message.PubKey = pubkeyFull
	message.PrivKeyBlock = ssh.Marshal(pk1)

	magic = append(magic, ssh.Marshal(message)...)

	return magic, nil
}
