package rclone_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/utkuozdemir/pv-migrate/internal/rclone"
)

func TestInterpret(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		code int
		want string
	}{
		// Code 1 deliberately gets no meaning: rclone was observed exiting 1 for
		// a credentials failure, so its documented "syntax or usage error" would
		// mislead next to the log tail that names the real cause.
		"usage error":   {code: 1, want: ""},
		"no directory":  {code: 3, want: `rclone documents this exit code as: Directory not found`},
		"no file":       {code: 4, want: `rclone documents this exit code as: File not found`},
		"success":       {code: 0, want: ""},
		"uncategorised": {code: 2, want: ""},
		"fatal":         {code: 7, want: ""},
		"newer code":    {code: 10, want: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, rclone.Interpret(tt.code))
		})
	}
}
