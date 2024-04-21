package progress

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	progressRegex = regexp.MustCompile(`\s*(?P<bytes>[0-9]+(,[0-9]+)*)\s+(?P<percentage>[0-9]{1,3})%`)
	rsyncEndRegex = regexp.MustCompile(`\s*total size is (?P<bytes>[0-9]+(,[0-9]+)*)`)
)

const (
	percentHundred = 100

	bytesTransferredIntBase   = 10
	bytesTransferredInt64Bits = 64
)

type Progress struct {
	Line        string
	Percentage  int
	Transferred int64
	Total       int64
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

	total := int64((float64(transferred) / float64(percentage)) * percentHundred)

	if transferred > total {
		// in case of a rounding error, update total, since transferred is more accurate
		total = transferred
	}

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
