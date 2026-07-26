package progress

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/utkuozdemir/pv-migrate/internal/progresslog"
)

var (
	// The percentage alternation stops at 100 on purpose. rsync computes the figure
	// without clamping it, so it can exceed 100 when the size it ends up
	// transferring differs from the total it started with, and there is no way to
	// tell such a line apart from a file name that happens to look like progress.
	// Either way it is not a reading the consumers can use, so it does not match.
	progressRegex = regexp.MustCompile(
		`\s*(?P<bytes>[0-9]+(,[0-9]+)*)\s+(?P<percentage>100|[0-9]{1,2})%`,
	)
	rsyncEndRegex = regexp.MustCompile(`\s*total size is (?P<bytes>[0-9]+(,[0-9]+)*)`)
)

const (
	percentHundred = 100

	bytesTransferredIntBase   = 10
	bytesTransferredInt64Bits = 64
)

type Progress = progresslog.Update

// FindLast returns the last progress entry in text, which is a chunk of the job
// pod's log. Rsync overwrites its progress line in place with a carriage return,
// so a chunk holds many entries and only the newest is wanted.
//
// The splitting and the "last line that parsed" choice belong to progresslog,
// which is also what the rclone side uses, so both report progress the same way
// and there is one place where that behaviour lives.
func FindLast(text string) Progress {
	return progresslog.FindLast(text, ParseLine)
}

func ParseLine(line string) (Progress, error) {
	endMatches := findNamedMatches(rsyncEndRegex, line)
	if len(endMatches) > 0 {
		total, err := parseNumBytes(endMatches["bytes"])
		if err != nil {
			return Progress{}, err
		}

		return Progress{
			Line:        line,
			Percentage:  percentHundred,
			Transferred: total,
			Total:       total,
		}, nil
	}

	prMatches := findNamedMatches(progressRegex, line)
	if len(prMatches) == 0 {
		return Progress{}, errors.New("no match")
	}

	percentage, err := strconv.Atoi(prMatches["percentage"])
	if err != nil {
		return Progress{}, fmt.Errorf("cannot parse percentage: %w", err)
	}

	if percentage == 0 {
		return Progress{
			Line:        line,
			Percentage:  0,
			Transferred: 0,
			Total:       0,
		}, nil
	}

	transferred, err := parseNumBytes(prMatches["bytes"])
	if err != nil {
		return Progress{}, err
	}

	return Progress{
		Line:        line,
		Percentage:  percentage,
		Transferred: transferred,
		Total:       estimateTotal(transferred, percentage),
	}, nil
}

// estimateTotal scales the transferred byte count back up by the reported
// percentage, since rsync's progress output carries no total of its own.
//
// The result is floored at transferred, because transferred is exact while the
// percentage is rounded to a whole number.
//
// It falls back to transferred when the scaled value would not fit. That is
// checked on the scaled float rather than on the input, because the scaling is
// done in floating point and rounds up: an input small enough to pass an integer
// bound can still produce a float at or above the limit. An out-of-range
// float-to-int conversion is implementation-defined in Go, so letting one through
// means the same log line reports a different total on each architecture.
// float64(math.MaxInt64) is exactly 2^63, so anything below it converts in range.
func estimateTotal(transferred int64, percentage int) int64 {
	if percentage <= 0 {
		return transferred
	}

	scaled := (float64(transferred) / float64(percentage)) * percentHundred
	if scaled >= float64(math.MaxInt64) {
		return transferred
	}

	return max(transferred, int64(scaled))
}

func parseNumBytes(numBytes string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.ReplaceAll(numBytes, ",", ""),
		bytesTransferredIntBase, bytesTransferredInt64Bits)
	if err != nil {
		return 0, fmt.Errorf("cannot parse number of bytes: %w", err)
	}

	return parsed, nil
}

func findNamedMatches(r *regexp.Regexp, str string) map[string]string {
	results := map[string]string{}

	match := r.FindStringSubmatch(str)
	for i, name := range match {
		results[r.SubexpNames()[i]] = name
	}

	return results
}
