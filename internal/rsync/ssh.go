package rsync

import (
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/ssh"
	"math/rand"
	"strings"
)

type SSHKeyAlgorithm string

const (
	RSAKeyAlgorithm     = "rsa"
	Ed25519KeyAlgorithm = "ed25519"
)

var (
	SSHKeyAlgorithms = []string{RSAKeyAlgorithm, Ed25519KeyAlgorithm}
)

func CreateSSHKeyPair(keyAlgorithm string) (string, string, error) {
	switch keyAlgorithm {
	case RSAKeyAlgorithm:
		return createSSHRSAKeyPair()
	case Ed25519KeyAlgorithm:
		return createSSHEd25519KeyPair()
	default:
		return "", "", fmt.Errorf("unexpected key algorithm: %v", keyAlgorithm)
	}
}

func createSSHRSAKeyPair() (string, string, error) {
	privateKey, err := rsa.GenerateKey(crand.Reader, 2048)
	if err != nil {
		return "", "", err
	}

	// generate and write private key as PEM
	var privKeyBuf strings.Builder

	privateKeyPEM := &pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}
	if err := pem.Encode(&privKeyBuf, privateKeyPEM); err != nil {
		return "", "", err
	}

	// generate and write public key
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", err
	}

	var pubKeyBuf strings.Builder
	pubKeyBuf.Write(ssh.MarshalAuthorizedKey(pub))

	return pubKeyBuf.String(), privKeyBuf.String(), nil
}

func createSSHEd25519KeyPair() (string, string, error) {
	pubKey, privateKey, err := ed25519.GenerateKey(crand.Reader)

	if err != nil {
		return "", "", err
	}

	// generate and write private key as PEM
	var privKeyBuf strings.Builder

	privateKeyPEM := &pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: marshalED25519PrivateKey(privateKey)}
	if err := pem.Encode(&privKeyBuf, privateKeyPEM); err != nil {
		return "", "", err
	}

	pub, _ := ssh.NewPublicKey(pubKey)
	var pubKeyBuf strings.Builder
	pubKeyBuf.Write(ssh.MarshalAuthorizedKey(pub))

	return pubKeyBuf.String(), privKeyBuf.String(), nil
}

// marshalED25519PrivateKey is taken as-is from https://github.com/mikesmitty/edkey
func marshalED25519PrivateKey(key ed25519.PrivateKey) []byte {
	magic := append([]byte("openssh-key-v1"), 0)

	var w struct {
		CipherName   string
		KdfName      string
		KdfOpts      string
		NumKeys      uint32
		PubKey       []byte
		PrivKeyBlock []byte
	}

	pk1 := struct {
		Check1  uint32
		Check2  uint32
		Keytype string
		Pub     []byte
		Priv    []byte
		Comment string
		Pad     []byte `ssh:"rest"`
	}{}

	ci := rand.Uint32()

	pk1.Check1 = ci
	pk1.Check2 = ci
	pk1.Keytype = ssh.KeyAlgoED25519

	pk, ok := key.Public().(ed25519.PublicKey)
	if !ok {
		//fmt.Fprintln(os.Stderr, "ed25519.PublicKey type assertion failed on an ed25519 public key. This should never ever happen.")
		return nil
	}
	pubKey := []byte(pk)

	pk1.Pub = pubKey
	pk1.Priv = key
	pk1.Comment = ""

	bs := 8
	blockLen := len(ssh.Marshal(pk1))
	padLen := (bs - (blockLen % bs)) % bs
	pk1.Pad = make([]byte, padLen)

	for i := 0; i < padLen; i++ {
		pk1.Pad[i] = byte(i + 1)
	}

	prefix := []byte{0x0, 0x0, 0x0, 0x0b}
	prefix = append(prefix, []byte(ssh.KeyAlgoED25519)...)
	prefix = append(prefix, []byte{0x0, 0x0, 0x0, 0x20}...)

	w.CipherName = "none"
	w.KdfName = "none"
	w.KdfOpts = ""
	w.NumKeys = 1
	w.PubKey = append(prefix, pubKey...)
	w.PrivKeyBlock = ssh.Marshal(pk1)

	magic = append(magic, ssh.Marshal(w)...)
	return magic
}
