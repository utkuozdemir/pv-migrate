//nolint:revive // suppress "var-naming: avoid meaningless package names"
package util

import (
	"math/rand/v2"
	"net/netip"
)

var letters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

// RandomString returns a random lowercase alphanumeric string of given length.
func RandomString(length int) string {
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

func ConvertStrings[TO, FROM ~string](values []FROM) []TO {
	s := make([]TO, 0, len(values))
	for _, v := range values {
		s = append(s, TO(v))
	}

	return s
}
