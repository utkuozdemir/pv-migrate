package util

import (
	"net/netip"
)

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
