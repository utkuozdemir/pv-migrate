package strategy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatSSHTargetHost(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1.2.3.4", formatSSHTargetHost("1.2.3.4"))
	assert.Equal(t, "example.com", formatSSHTargetHost("example.com"))
	assert.Equal(t, "[2001:0db8:85a3:0000:0000:8a2e:0370:7334]",
		formatSSHTargetHost("2001:0db8:85a3:0000:0000:8a2e:0370:7334"))
	assert.Equal(t, "[::1]", formatSSHTargetHost("::1"))
}

// TestFormatSSHTargetHostIsIdempotent is what lets --dest-host-override go
// through the same formatting as a resolved address. The override used to replace
// the resolved host after it had been bracketed, so an IPv6 literal passed to it
// reached rsync unbracketed, and rsync split the remote spec on the literal's own
// first colon and looked for a host called "fe80".
func TestFormatSSHTargetHostIsIdempotent(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"1.2.3.4", "example.com", "::1", "[::1]", "fe80::1%eth0", ""} {
		once := formatSSHTargetHost(host)
		assert.Equal(t, once, formatSSHTargetHost(once),
			"formatting %q twice must not differ from formatting it once", host)
	}
}
