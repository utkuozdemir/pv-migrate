package progress

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/utkuozdemir/pv-migrate/internal/progresslog"
)

var (
	progressRegex = regexp.MustCompile(
		`\s*(?P<bytes>[0-9]+(,[0-9]+)*)\s+(?P<percentage>[0-9]{1,3})%`,
	)
	rsyncEndRegex = regexp.MustCompile(`\s*total size is (?P<bytes>[0-9]+(,[0-9]+)*)`)
)

const (
	percentHundred = 100

	bytesTransferredIntBase   = 10
	bytesTransferredInt64Bits = 64
)

type Progress = progresslog.Update

// FindLast returns the last progress entry found anywhere in text.
// Rsync uses \r to overwrite progress in-place, so a single log line
// may contain many concatenated progress entries.
func FindLast(text string) Progress {
	matches := progressRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return Progress{}
	}

	last := matches[len(matches)-1]

	names := progressRegex.SubexpNames()
	named := make(map[string]string, len(names))

	for i, name := range names {
		named[name] = last[i]
	}

	percentage, err := strconv.Atoi(named["percentage"])
	if err != nil || percentage == 0 {
		return Progress{}
	}

	transferred, err := parseNumBytes(named["bytes"])
	if err != nil {
		return Progress{}
	}

	total := max(transferred, int64((float64(transferred)/float64(percentage))*percentHundred))

	return Progress{
		Percentage:  percentage,
		Transferred: transferred,
		Total:       total,
	}
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

	// in case of a rounding error, update total, since transferred is more accurate
	total := max(transferred, int64((float64(transferred)/float64(percentage))*percentHundred))

	return Progress{
		Line:        line,
		Percentage:  percentage,
		Transferred: transferred,
		Total:       total,
	}, nil
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
