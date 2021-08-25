package util

import (
	"math/rand"
	"net"
	"time"
)

var (
	letters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")
	random  = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
)

// RandomHexadecimalString returns a random lowercase hexadecimal string of given length.
func RandomHexadecimalString(length int) string {
	b := make([]rune, length)
	for i := range b {
		b[i] = letters[random.Intn(len(letters))]
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
