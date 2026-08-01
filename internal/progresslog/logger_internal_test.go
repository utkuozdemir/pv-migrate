package progresslog

import (
	"bytes"
	"log/slog"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var renderedPercentage = regexp.MustCompile(`(\d+)%`)

// TestProgressBarReachesItsEnd drives the bar the way rsync drives it, with a
// percentage that arrives rounded to a whole number and repeats itself while
// bytes keep moving. Every painted frame must be at least the one before it,
// and the completion the caller triggers must paint a full bar.
func TestProgressBarReachesItsEnd(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := NewLogger(LoggerOptions{Writer: &buf, ShowProgressBar: true})
	bar := logger.progressBarOnce()
	require.NotNil(t, bar)

	// Ten updates per whole percentage point. That is how a transfer behaves,
	// with bytes still arriving between two steps of the stated percentage.
	for tenths := 1; tenths <= 1000; tenths++ {
		require.NoError(t, logger.updateProgressBar(bar, tenths/10))
	}

	logger.barTransferred = 1 // the bar drew something, so completion applies
	logger.FinishBar(slog.New(slog.DiscardHandler))

	painted := paintedPercentages(t, buf.String())

	require.NotEmpty(t, painted)
	assert.Equal(t, 100, painted[len(painted)-1], "the bar reaches its end")

	for i := 1; i < len(painted); i++ {
		assert.GreaterOrEqual(t, painted[i], painted[i-1],
			"frame %d moved backward: %v", i, painted[max(0, i-4):i+1])
	}
}

// TestProgressBarSurvivesAnEarlyHundred covers the rclone shape, where the
// mover can report everything transferred and then find more work. Reaching the
// bar's end finishes it for good in the library, so a reported hundred must not
// be allowed to do that while the job is still running.
func TestProgressBarSurvivesAnEarlyHundred(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := NewLogger(LoggerOptions{Writer: &buf, ShowProgressBar: true})
	bar := logger.progressBarOnce()
	require.NotNil(t, bar)

	require.NoError(t, logger.updateProgressBar(bar, 100))
	require.False(t, bar.IsFinished(), "a reported hundred must not finish the bar")

	// The total grew, so the percentage legitimately falls back and then climbs.
	for _, percentage := range []int{40, 60, 80, 100} {
		require.NoError(t, logger.updateProgressBar(bar, percentage))
	}

	painted := paintedPercentages(t, buf.String())

	require.Contains(t, painted, 40, "updates after the early hundred still paint")
	assert.Equal(t, 99, painted[len(painted)-1], "the end is left to the caller")
}

func paintedPercentages(t *testing.T, rendered string) []int {
	t.Helper()

	matches := renderedPercentage.FindAllStringSubmatch(rendered, -1)
	percentages := make([]int, 0, len(matches))

	for _, match := range matches {
		value, err := strconv.Atoi(match[1])
		require.NoError(t, err)

		percentages = append(percentages, value)
	}

	return percentages
}
