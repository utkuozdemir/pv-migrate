package progress_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/rsync/progress"
)

func TestParseLogLineEndMatch(t *testing.T) {
	t.Parallel()

	l := "total size is 1,879,048,192  speedup is 31,548.30"
	p, err := progress.ParseLine(l)
	require.NoError(t, err)
	assert.Equal(t, 100, p.Percentage)
	assert.Equal(t, int64(1879048192), p.Transferred)
	assert.Equal(t, int64(1879048192), p.Total)
}

func TestParseLineProgress(t *testing.T) {
	t.Parallel()

	l := "     52,428,800  35%  240.08kB/s    0:03:33 (xfr#1, to-chk=7/9)"
	p, err := progress.ParseLine(l)
	require.NoError(t, err)
	assert.Equal(t, 35, p.Percentage)
	assert.Equal(t, int64(52428800), p.Transferred)
}

func TestParseLineZeroPercent(t *testing.T) {
	t.Parallel()

	l := "          0   0%    0.00kB/s    0:00:00"
	p, err := progress.ParseLine(l)
	require.NoError(t, err)
	assert.Equal(t, 0, p.Percentage)
	assert.Equal(t, int64(0), p.Transferred)
}

func TestParseLineNoMatch(t *testing.T) {
	t.Parallel()

	_, err := progress.ParseLine("./")
	require.ErrorContains(t, err, "no match")
}

func TestFindLast(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		wantPct   int
		wantXfer  int64
		wantTotal int64
	}{
		{
			name:    "empty text",
			text:    "",
			wantPct: 0,
		},
		{
			name:    "no progress entries",
			text:    "./\ns50mb.dat\ndir1/\n",
			wantPct: 0,
		},
		{
			name:      "single entry",
			text:      "     52,428,800  35%  240.08kB/s    0:03:33",
			wantPct:   35,
			wantXfer:  52428800,
			wantTotal: 149796571,
		},
		{
			// Simulates rsync \r-separated progress within a single log line
			name: "multiple entries picks last",
			text: "     32,768   0%    0.00kB/s    0:00:00" +
				"\r     294,912   1%  242.19kB/s    0:10:04" +
				"\r  52,428,800  35%  240.08kB/s    0:03:33",
			wantPct:   35,
			wantXfer:  52428800,
			wantTotal: 149796571,
		},
		{
			name: "multiline with file names between progress",
			text: "s50mb.dat\n" +
				"     52,428,800  35%  240.08kB/s    0:03:33 (xfr#1, to-chk=7/9)\n" +
				"dir1/s10mb.dat\n" +
				"     62,914,560  42%  240.07kB/s    0:04:15 (xfr#2, to-chk=5/9)\n",
			wantPct:   42,
			wantXfer:  62914560,
			wantTotal: 149796571,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := progress.FindLast(tt.text)
			assert.Equal(t, tt.wantPct, p.Percentage)
			assert.Equal(t, tt.wantXfer, p.Transferred)

			if tt.wantTotal > 0 {
				assert.Equal(t, tt.wantTotal, p.Total)
			}
		})
	}
}
