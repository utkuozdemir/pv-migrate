package rsync_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/rsync"
)

func TestBuildRejectsSSHOnBothSides(t *testing.T) {
	t.Parallel()

	cmd := rsync.Cmd{SrcUseSSH: true, DestUseSSH: true}

	_, err := cmd.Build()
	require.Error(t, err)
}

func TestBuildRemoteSpecs(t *testing.T) {
	t.Parallel()

	pull, err := (&rsync.Cmd{
		SrcUseSSH: true, SrcSSHHost: "rel-sshd.ns", SrcPath: "/source/",
		DestPath: "/dest/",
	}).Build()
	require.NoError(t, err)
	assert.Contains(t, pull, "'root@rel-sshd.ns:/source/' '/dest/'",
		"pull mode reads from the remote and writes locally")

	push, err := (&rsync.Cmd{
		DestUseSSH: true, DestSSHHost: "rel-sshd.ns", DestSSHUser: "pvmigrate",
		SrcPath: "/source/", DestPath: "/dest/",
	}).Build()
	require.NoError(t, err)
	assert.Contains(t, push, "'/source/' 'pvmigrate@rel-sshd.ns:/dest/'",
		"push mode reads locally and writes to the remote")
}

// TestBuildRejectsUnrenderableCharacters pins the reason these are refused rather
// than quoted: the built command is interpolated into a YAML block scalar in the
// Job template, so such a character does not produce a broken command, it produces
// a chart that will not render, and the error the user sees points at a template
// line instead of at their own flag.
//
// The set is wider than a newline because YAML permits no control character on
// that line except tab, and it reads two of the C1 controls and two Unicode
// separators as line breaks of their own. Each case here was checked to make the
// rendered manifest unparseable.
func TestBuildRejectsUnrenderableCharacters(t *testing.T) {
	t.Parallel()

	for name, bad := range map[string]string{
		"newline":             "\n",
		"carriage return":     "\r",
		"nul":                 "\x00",
		"bell":                "\a",
		"vertical tab":        "\v",
		"escape":              "\x1b",
		"delete":              "\x7f",
		"next line":           "\u0085",
		"c1 control":          "\u0090",
		"line separator":      "\u2028",
		"paragraph separator": "\u2029",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for field, cmd := range map[string]rsync.Cmd{
				"source path": {SrcPath: "/source/a" + bad + "b/", DestPath: "/dest/"},
				"dest path":   {SrcPath: "/source/", DestPath: "/dest/a" + bad + "b/"},
				"ssh host": {
					DestUseSSH: true, DestSSHHost: "host" + bad + "name",
					SrcPath: "/s/", DestPath: "/d/",
				},
			} {
				_, err := cmd.Build()
				require.ErrorContains(t, err, "must not contain the character", "field %s", field)
			}
		})
	}
}

// TestBuildAcceptsTab is the one control character that stays allowed: YAML
// accepts it inside a block scalar, single quotes preserve it, and a file name may
// legitimately contain one.
func TestBuildAcceptsTab(t *testing.T) {
	t.Parallel()

	_, err := (&rsync.Cmd{SrcPath: "/source/a\tb/", DestPath: "/dest/"}).Build()
	require.NoError(t, err)
}

// TestBuiltCommandArgvThroughShell is the test that the source and destination
// specs survive as written.
//
// The chart runs the built command through `sh -c`, so the only oracle that
// proves anything is a shell. This runs the real thing with rsync shadowed by a
// script that prints its arguments one per line, and compares that against the
// specs the command was built from. Before the paths were quoted, a path
// containing a space split into two arguments here, which is what made a
// --source-path with a space in it unusable.
func TestBuiltCommandArgvThroughShell(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the built command is only ever run by the Linux job container's shell")
	}

	paths := []string{
		"/source/plain/",
		"/source/my dir/",
		"/source/it's mine/",
		`/source/$HOME/`,
		"/source/a;rm -rf x/",
		"/source/back`tick`/",
		`/source/back\slash/`,
		"/source/glob*?[a]/",
		"/source/tab\there/",
		"/source/двойка/",
		"/source/quote\"dquote/",
		"/source/pipe|and&/",
		"/source/(paren)/",
		"/source/new{brace}/",
		"/source/#hash/",
		"/source/~tilde/",
	}

	for _, srcPath := range paths {
		t.Run(srcPath, func(t *testing.T) {
			t.Parallel()

			cmd := rsync.Cmd{
				SrcUseSSH: true, SrcSSHHost: "sshd.ns", SrcSSHUser: "root",
				SrcPath:  srcPath,
				DestPath: "/dest/out dir/",
			}

			built, err := cmd.Build()
			require.NoError(t, err)

			argv := shellArgv(t, built)

			require.GreaterOrEqual(t, len(argv), 2)
			assert.Equal(t, "root@sshd.ns:"+srcPath, argv[len(argv)-2],
				"the source spec did not survive the shell as one argument")
			assert.Equal(t, "/dest/out dir/", argv[len(argv)-1],
				"the destination spec did not survive the shell as one argument")

			// The -e value is one argument too, so ssh and its options do not leak
			// into rsync's own argument list.
			assert.Contains(t, argv, "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "+
				"-o ConnectTimeout=5 -o ServerAliveInterval=10 -o ServerAliveCountMax=3")
		})
	}
}

// shellArgv runs command through /bin/sh with rsync replaced by a script that
// prints each argument on its own line, and returns those arguments.
func shellArgv(t *testing.T, command string) []string {
	t.Helper()

	dir := t.TempDir()

	const printArgs = "#!/bin/sh\nfor arg in \"$@\"; do printf '%s\\n' \"$arg\"; done\n"

	//nolint:gosec // it has to be executable for the shell to find it
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rsync"), []byte(printArgs), 0o700))

	// PATH is replaced rather than prepended so the real rsync cannot be reached
	// even if the built command were to lose its leading word.
	shell := exec.CommandContext(t.Context(), "/bin/sh", "-c", command)
	shell.Env = []string{"PATH=" + dir}

	out, err := shell.Output()
	require.NoError(t, err, "the built command is not runnable: %s", command)

	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}
