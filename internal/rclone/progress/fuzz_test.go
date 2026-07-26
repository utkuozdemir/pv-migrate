package progress_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/rclone/progress"
)

// FuzzParseLine holds this parser to the same contract as the rsync one, since
// both feed the same status output and the same progress bar. The input is
// rclone's JSON log, so the values arrive already typed and the interesting
// inputs are the ones that are well-formed JSON but not well-formed statistics.
func FuzzParseLine(f *testing.F) {
	f.Add(`{"level":"info","stats":{"bytes":512,"totalBytes":1024}}`)
	f.Add(`{"stats":{"bytes":1024,"totalBytes":1024}}`)
	f.Add(`{"level":"info","msg":"no stats here"}`)
	f.Add(`not json at all`)
	// Negative counts used to be reported as a negative percentage, and a negative
	// total reached the progress bar, after which every update it was given failed
	// and was logged as a warning once a second for the rest of the transfer.
	f.Add(`{"stats":{"bytes":-500,"totalBytes":1000}}`)
	f.Add(`{"stats":{"bytes":500,"totalBytes":-1000}}`)
	f.Add(`{"stats":{"bytes":-500,"totalBytes":-1000}}`)
	// More transferred than the total: rclone revises its total upwards as it
	// walks, so a stats line can briefly overshoot.
	f.Add(`{"stats":{"bytes":9223372036854775807,"totalBytes":1}}`)

	f.Fuzz(func(t *testing.T, line string) {
		got, err := progress.ParseLine(line)
		if err != nil {
			return
		}

		requireUsable(t, got)
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
