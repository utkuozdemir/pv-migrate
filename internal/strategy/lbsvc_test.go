package strategy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatSSHTargetHost(t *testing.T) {
	assert.Equal(t, "1.2.3.4", formatSSHTargetHost("1.2.3.4"))
	assert.Equal(t, "example.com", formatSSHTargetHost("example.com"))
	assert.Equal(t, "[2001:0db8:85a3:0000:0000:8a2e:0370:7334]",
		formatSSHTargetHost("2001:0db8:85a3:0000:0000:8a2e:0370:7334"))
	assert.Equal(t, "[::1]", formatSSHTargetHost("::1"))
}
