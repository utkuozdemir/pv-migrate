package bucketstorage

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Detecting a dry run means reading a raw shell fragment the user handed to
// rclone, which cannot be parsed properly. But once it is split into words, the
// question of what those words mean is not a guess: rclone parses them with
// pflag. So pflag is the oracle here, rather than a table of expectations written
// out by hand, which is what got the first attempt at this wrong in both
// directions.
//
// The vocabulary is deliberately small and made only of tokens pflag accepts,
// because pflag is only a valid oracle inside that domain. rclone has hundreds of
// flags and the heuristic has to tolerate all of them; what it must not do is
// disagree with pflag about the ones that decide a dry run.
var dryRunTokens = []string{
	"-n", "--dry-run",
	"-n=true", "-n=false", "-n=1", "-n=0",
	"--dry-run=true", "--dry-run=false", "--dry-run=TRUE", "--dry-run=0", "--dry-run=t",
	"-v", "-P", "-nv", "-vn", "-Pnv",
	// pflag binds an assigned value to the LAST shorthand in a bundle and sets the
	// earlier ones bare, so the position of n inside the bundle decides the answer.
	"-vn=true", "-vn=false", "-nv=true", "-nv=false", "-Pvn=0", "-nvn=false",
	"--verbose", "--progress",
}

// pflagDryRun reports what rclone would end up with for these arguments, and
// whether it would accept them at all.
//
// The flags are declared with the types rclone declares them with, since a
// mistyped one would make this an oracle for something rclone does not do. Verbose
// counts rather than toggles, which is why an assignment to it is refused.
func pflagDryRun(t *testing.T, args []string) (bool, bool) {
	t.Helper()

	flags := pflag.NewFlagSet("rclone", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)

	dryRun := flags.BoolP("dry-run", "n", false, "")
	flags.CountP("verbose", "v", "")
	flags.BoolP("progress", "P", false, "")
	flags.BoolP("quiet", "q", false, "")

	if err := flags.Parse(args); err != nil {
		return false, false
	}

	return *dryRun, true
}

func TestHasRcloneDryRunAgreesWithPflag(t *testing.T) {
	t.Parallel()

	// Every one- and two-token combination of the vocabulary. Two is enough to
	// cover the ordering question, which is where the first implementation failed.
	args := make([]string, 0, (1+len(dryRunTokens))*len(dryRunTokens))

	for _, first := range dryRunTokens {
		args = append(args, first)
		for _, second := range dryRunTokens {
			args = append(args, first+" "+second)
		}
	}

	checked := 0

	for _, extraArgs := range args {
		want, usable := pflagDryRun(t, strings.Fields(extraArgs))
		if !usable {
			continue
		}

		assert.Equal(t, want, hasRcloneDryRun(extraArgs),
			"disagrees with pflag for %q", extraArgs)

		checked++
	}

	require.NotZero(t, checked)
	t.Logf("checked %d argument combinations against pflag", checked)
}

// TestHasRcloneDryRunToleratesOtherFlags covers the rest of the input domain, where
// pflag is not an oracle because it does not know rclone's other flags. What is
// required of these is only that an ordinary backup is not read as a dry run, since
// that would leave the backup without the metadata identifying it.
func TestHasRcloneDryRunToleratesOtherFlags(t *testing.T) {
	t.Parallel()

	for _, extraArgs := range []string{
		"",
		"--transfers 8",
		"--transfers -1n",
		"--bwlimit 1n",
		"--exclude nope",
		// a dash-led value is only misread when it is letters throughout
		"--include -no-cache",
		"--exclude -1n",
		"--dry-run=notabool",
		"--checksum --fast-list",
		"--backup-dir /tmp/x",
	} {
		assert.False(t, hasRcloneDryRun(extraArgs), "%q should not read as a dry run", extraArgs)
	}
}

// TestHasRcloneDryRunMisreadsFlagShapedValues records where this stops working, so
// that the limit is written down rather than discovered again.
//
// A flag that takes a value consumes whatever token follows it, including one that
// begins with a dash. Reading the arguments word by word cannot see that, so a
// value shaped like a short flag bundle is read as one. Every case below is an
// ordinary backup that will be treated as a dry run and lose its metadata.
//
// Narrowing this would mean knowing which of rclone's several hundred flags take a
// value, which is an inventory that goes stale. The way out is not a better guess:
// it is to stop guessing, by taking a dry run as an option of this tool and passing
// it on, at which point none of this code needs to exist.
func TestHasRcloneDryRunMisreadsFlagShapedValues(t *testing.T) {
	t.Parallel()

	for _, extraArgs := range []string{
		"--exclude -n",
		"--exclude -nv",
		"--backup-dir -nightly",
		// pflag stops reading flags at a bare --, and this does not
		"-- -n",
	} {
		assert.True(t, hasRcloneDryRun(extraArgs),
			"%q is a known false positive, update this test if that changes", extraArgs)
	}
}
