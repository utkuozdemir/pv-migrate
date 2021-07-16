package k8s

import (
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

const (
	logBlock = `
file-10.dat
    134,217,728   7%  369.94MB/s    0:00:00 (xfr#2, to-chk=26/29)
file-11.dat
    201,326,592  10%  373.54MB/s    0:00:00 (xfr#3, to-chk=25/29)
file-12.dat
    268,435,456  14%  373.72MB/s    0:00:00 (xfr#4, to-chk=24/29)
file-13.dat
    335,544,320  17%  373.40MB/s    0:00:00 (xfr#5, to-chk=23/29)
file-14.dat
    402,653,184  21%  373.90MB/s    0:00:01 (xfr#6, to-chk=22/29)
file-15.dat
`
)

func TestGetLatestProgress(t *testing.T) {
	logs := strings.Split(logBlock, "\n")
	p, err := getLatestProgress(logs)
	assert.NoError(t, err)
	assert.Equal(t, 21, p.percentage)
	assert.Equal(t, int64(402653184), p.transferred)
	assert.Equal(t, int64(1917396114), p.total)
}

func TestParseLogLineEndMatch(t *testing.T) {
	l := "total size is 1,879,048,192  speedup is 31,548.30"
	p, err := parseLogLine(&l)
	assert.NoError(t, err)
	assert.Equal(t, 100, p.percentage)
	assert.Equal(t, int64(1879048192), p.transferred)
	assert.Equal(t, int64(1879048192), p.total)
}
