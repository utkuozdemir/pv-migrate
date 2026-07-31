package rsync_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/utkuozdemir/pv-migrate/internal/rsync"
)

func TestInterpret(t *testing.T) {
	t.Parallel()

	const documented = `rsync documents this exit code as: %s`

	tests := map[string]struct {
		code int
		want string
	}{
		"usage error":  {code: 1, want: fmt.Sprintf(documented, "Syntax or usage error")},
		"partial":      {code: 23, want: fmt.Sprintf(documented, "Partial transfer due to error")},
		"timeout":      {code: 30, want: fmt.Sprintf(documented, "Timeout in data send/receive")},
		"success":      {code: 0, want: ""},
		"undocumented": {code: 99, want: ""},
		"negative":     {code: -1, want: ""},
		"oom kill":     {code: 137, want: ""},
		"vanished": {
			code: rsync.VanishedFilesExitCode,
			want: fmt.Sprintf(documented, "Partial transfer due to vanished source files"),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, rsync.Interpret(tt.code))
		})
	}
}

// Test255IsAttributedToTheRemoteShell pins the one row that is not rsync's:
// rsync passes the remote shell's status through, so claiming 255 as an rsync
// meaning would be inventing a cause.
func TestInterpret255IsAttributedToTheRemoteShell(t *testing.T) {
	t.Parallel()

	got := rsync.Interpret(255)

	assert.Contains(t, got, "not an rsync exit value")
	assert.Contains(t, got, "remote shell")
}
