package util

import "math/rand"

var letters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

func RandomHexadecimalString(length int) string {
	b := make([]rune, length)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
