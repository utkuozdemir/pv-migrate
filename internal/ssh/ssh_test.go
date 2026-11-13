package ssh_test

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"

	"github.com/utkuozdemir/pv-migrate/internal/ssh"
)

func TestCreateSSHKeyPair(t *testing.T) {
	t.Parallel()

	tests := []struct {
		algorithm string
		wantType  string
	}{
		{algorithm: "ed25519", wantType: gossh.KeyAlgoED25519},
		{algorithm: "rsa", wantType: gossh.KeyAlgoRSA},
	}

	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			t.Parallel()

			pubStr, privStr, err := ssh.CreateSSHKeyPair(tt.algorithm)
			require.NoError(t, err)

			pubKey, _, _, rest, err := gossh.ParseAuthorizedKey([]byte(pubStr))
			require.NoError(t, err)
			assert.Empty(t, rest)
			assert.Equal(t, tt.wantType, pubKey.Type())

			signer, err := gossh.ParsePrivateKey([]byte(privStr))
			require.NoError(t, err)
			assert.Equal(t, pubKey.Marshal(), signer.PublicKey().Marshal())

			payload := []byte("pv-migrate-ssh-key-roundtrip")
			sig, err := signer.Sign(rand.Reader, payload)
			require.NoError(t, err)
			require.NoError(t, pubKey.Verify(payload, sig))
		})
	}
}

func TestCreateSSHKeyPairRejectsUnsupportedAlgorithm(t *testing.T) {
	t.Parallel()

	pubStr, privStr, err := ssh.CreateSSHKeyPair("dsa")
	require.ErrorContains(t, err, "unsupported key algorithm: dsa")
	assert.Empty(t, pubStr)
	assert.Empty(t, privStr)
}
