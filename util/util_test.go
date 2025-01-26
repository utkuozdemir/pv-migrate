package util_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/utkuozdemir/pv-migrate/util"
)

func TestIsIPv6(t *testing.T) {
	t.Parallel()

	assert.False(t, util.IsIPv6("192.168.1.1"))
	assert.True(t, util.IsIPv6("2001:0db8:85a3:0000:0000:8a2e:0370:7334"))
	assert.True(t, util.IsIPv6("::1"))
}
