package progress_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/rclone/progress"
)

func TestParseLine(t *testing.T) {
	t.Parallel()

	//nolint:lll
	line := `{"time":"2026-04-10T22:12:18.20656562Z","level":"notice","msg":"1.027 MiB / 5 MiB, 21%, 1.027 MiB/s, ETA 3s","stats":{"bytes":1077248,"totalBytes":5242880},"source":"slog/logger.go:256"}`

	got, err := progress.ParseLine(line)
	require.NoError(t, err)
	assert.Equal(t, 20, got.Percentage)
	assert.Equal(t, int64(1077248), got.Transferred)
	assert.Equal(t, int64(5242880), got.Total)
}

func TestParseLine_NoStats(t *testing.T) {
	t.Parallel()

	_, err := progress.ParseLine(`{"time":"2026-04-10T22:12:17Z","level":"notice","msg":"Config file not found"}`)
	require.ErrorContains(t, err, "no stats")
}

func TestParseLine_DoesNotRoundUpToComplete(t *testing.T) {
	t.Parallel()

	line := `{"stats":{"bytes":99,"totalBytes":100}}`

	got, err := progress.ParseLine(line)
	require.NoError(t, err)
	assert.Equal(t, 99, got.Percentage)
}

func TestFindLast(t *testing.T) {
	t.Parallel()

	//nolint:lll
	text := `+ rclone sync --config /etc/rclone/rclone.conf /data remote:bucket/path --stats 1s --stats-log-level NOTICE --use-json-log --stats-one-line
+ period=5
+ '[' 0 -le 3 ]
+ rclone sync --config /etc/rclone/rclone.conf /data remote:bucket/path --stats 1s --stats-log-level NOTICE --use-json-log --stats-one-line
{"time":"2026-04-10T22:12:18.20656562Z","level":"notice","msg":"1.027 MiB / 5 MiB, 21%, 1.027 MiB/s, ETA 3s","stats":{"bytes":1077248,"totalBytes":5242880},"source":"slog/logger.go:256"}
{"time":"2026-04-10T22:12:20.205448037Z","level":"notice","msg":"3.027 MiB / 5 MiB, 61%, 1.009 MiB/s, ETA 1s","stats":{"bytes":3174400,"totalBytes":5242880},"source":"slog/logger.go:256"}`

	got := progress.FindLast(text)
	assert.Equal(t, 60, got.Percentage)
	assert.Equal(t, int64(3174400), got.Transferred)
	assert.Equal(t, int64(5242880), got.Total)
}
