package util

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
)

var letters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

// RandomHexadecimalString returns a random lowercase hexadecimal string of given length.
func RandomHexadecimalString(length int) string {
	lengthBigInt := big.NewInt(int64(length))
	b := make([]rune, length)
	for i := range b {
		rnd, err := rand.Int(rand.Reader, lengthBigInt)
		if err != nil {
			panic(fmt.Sprintf("failed to generate random number: %v", err))
		}
		b[i] = letters[rnd.Int64()]
	}

	return string(b)
}

func IsIPv6(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return ip.To4() == nil
}
