//nolint:revive // suppress "var-naming: avoid meaningless package names"
package util

import (
	"math/rand/v2"
	"net/netip"
)

var letters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

// RandomHexadecimalString returns a random lowercase hexadecimal string of given length.
func RandomHexadecimalString(length int) string {
	runes := make([]rune, length)
	for i := range runes {
		runes[i] = letters[rand.IntN(len(letters))] //nolint:gosec // not security-sensitive
	}

	return string(runes)
}

func IsIPv6(host string) bool {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}

	return addr.Is6()
}
