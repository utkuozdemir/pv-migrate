package progress_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/rsync/progress"
)

// FuzzParseLine checks the contract the callers of a parsed progress update
// already rely on. The exec side of a migration reads whatever rsync, a busybox
// build of it, or a user's --rsync-extra-args cause to be printed, and an update
// that leaves this parser goes two places without further checking: into the
// numbers `pv-migrate status` prints, and into the progress bar, which rejects
// every later update once it has been given a nonsense maximum.
//
// So the properties are the ones those two consumers need: a percentage that is
// a percentage, and a total that is at least what has already been transferred.
func FuzzParseLine(f *testing.F) {
	f.Add("     32,768   0%    0.00kB/s    0:00:00")
	f.Add("  1,234,567  45%    1.00MB/s    0:00:01")
	f.Add("  1,234,567 100%    1.00MB/s    0:00:01 (xfr#1, to-chk=0/1)")
	f.Add("total size is 1,234,567  speedup is 1.00")
	f.Add("sending incremental file list")
	// Not rsync output: a three-digit percentage used to be accepted and reported
	// verbatim, so `pv-migrate status` would print 999%.
	f.Add("  1,234,567 999%    1.00MB/s    0:00:01")
	// Large enough that scaling by the percentage overflows int64. The scaled total
	// used to be an out-of-range float conversion, which Go leaves
	// implementation-defined, so the same line produced a different total on arm64
	// than on amd64.
	f.Add("  9,223,372,036,854,775,807   1%")
	f.Add("  92,233,720,368,547,759  1%")
	// A byte count too large for int64 at all.
	f.Add("  99,999,999,999,999,999,999  50%")

	f.Fuzz(func(t *testing.T, line string) {
		got, err := progress.ParseLine(line)
		if err != nil {
			return
		}

		requireUsable(t, got)

		// FindLast scans a whole log chunk, and status reports whatever it returns,
		// so it owes the same guarantees for a chunk made of one line.
		requireUsable(t, progress.FindLast(line))
	})
}

func requireUsable(t *testing.T, got progress.Progress) {
	t.Helper()

	require.GreaterOrEqual(t, got.Percentage, 0, "percentage below zero: %+v", got)
	require.LessOrEqual(t, got.Percentage, 100, "percentage above one hundred: %+v", got)
	require.GreaterOrEqual(t, got.Transferred, int64(0), "negative transferred: %+v", got)
	require.GreaterOrEqual(t, got.Total, got.Transferred, "total below transferred: %+v", got)
}
