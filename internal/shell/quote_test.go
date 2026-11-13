package shell_test

import (
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/shell"
)

// TestQuoteRoundTripsThroughPOSIXShell checks Quote against a real POSIX shell
// rather than a hand-written word splitter, so a blind spot in Quote cannot be
// shared by the test oracle.
func TestQuoteRoundTripsThroughPOSIXShell(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX sh")
	}

	cases := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "plain", input: "/source/data"},
		{name: "spaces", input: "/source/my dir/sub folder/"},
		{name: "leading and trailing spaces", input: "  spaced  "},
		{name: "tab", input: "/source/with\ttab"},
		{name: "newline", input: "line1\nline2"},
		{name: "carriage return", input: "line1\rline2"},
		{name: "single quote", input: "it's"},
		{name: "only single quotes", input: "'''"},
		{name: "escaped single quote sequence", input: `'"'"'`},
		{name: "double quotes", input: `"quoted"`},
		{name: "variable expansion", input: "$HOME ${PATH} $?"},
		{name: "command substitution", input: "$(echo injected)"},
		{name: "backtick substitution", input: "`echo injected`"},
		{name: "backslashes", input: `\a\\b\'\"`},
		{name: "globs", input: "* ? [a-z]"},
		{name: "tilde and hash", input: "~/dir #comment"},
		{name: "control operators", input: "a; b && c || d | e &"},
		{name: "redirections and subshells", input: "(a > /tmp/out < /dev/null)"},
		{name: "braces and bang", input: "{a,b} !hist"},
		{name: "option-like dash", input: "-n"},
		{name: "utf8", input: "/source/données/\u65e5\u672c\u8a9e/🚀"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			script := `set -- ` + shell.Quote(tc.input) + "\n" +
				`[ "$#" -eq 1 ] || exit 2` + "\n" +
				`printf '%s' "$1"`

			out, err := exec.CommandContext(t.Context(), "sh", "-c", script).Output()
			require.NoError(t, err)
			assert.Equal(t, tc.input, string(out))
		})
	}
}

func TestCheckSingleLine(t *testing.T) {
	t.Parallel()

	valid := []string{
		"",
		"/source/my dir/file's name",
		"/source/with\ttab",
		"/source/données/\u65e5\u672c\u8a9e/🚀",
		"\ufeffleading-bom-is-printable",
	}
	for _, val := range valid {
		require.NoError(t, shell.CheckSingleLine("--source-path", val), "expected %q to be accepted", val)
	}

	invalid := []struct {
		name  string
		input string
	}{
		{name: "invalid utf8", input: "bad\xffbyte"},
		{name: "nul", input: "nul\x00byte"},
		{name: "lf", input: "line1\nline2"},
		{name: "cr", input: "line1\rline2"},
		{name: "escape", input: "ansi\x1b[31m"},
		{name: "del", input: "del\x7fchar"},
		{name: "c1 next line", input: "nel\u0085break"},
		{name: "c1 upper bound", input: "apc\u009fchar"},
		{name: "unicode line separator", input: "ls\u2028break"},
		{name: "unicode paragraph separator", input: "ps\u2029break"},
		{name: "noncharacter fffe", input: "nonchar\ufffe"},
		{name: "noncharacter ffff", input: "nonchar\uffff"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := shell.CheckSingleLine("--source-path", tc.input)
			require.ErrorContains(t, err, "--source-path")
		})
	}
}
