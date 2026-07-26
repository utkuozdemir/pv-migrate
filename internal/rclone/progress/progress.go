package progress

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/utkuozdemir/pv-migrate/internal/progresslog"
)

const percentHundred = 100

type Progress = progresslog.Update

type logEntry struct {
	Stats *stats `json:"stats"`
}

type stats struct {
	Bytes      int64 `json:"bytes"`
	TotalBytes int64 `json:"totalBytes"`
}

func ParseLine(line string) (Progress, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Progress{}, errors.New("empty line")
	}

	var entry logEntry

	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return Progress{}, fmt.Errorf("cannot parse JSON log line: %w", err)
	}

	if entry.Stats == nil {
		return Progress{}, errors.New("no stats in log line")
	}

	// A byte count cannot be negative, and one that is would be reported as a
	// negative percentage and then handed to the progress bar, which rejects every
	// subsequent update once its maximum is negative. Refuse the line instead.
	if entry.Stats.Bytes < 0 || entry.Stats.TotalBytes < 0 {
		return Progress{}, errors.New("negative byte count in log line")
	}

	percentage := 0

	if entry.Stats.TotalBytes > 0 {
		if entry.Stats.Bytes >= entry.Stats.TotalBytes {
			percentage = percentHundred
		} else {
			percentage = int(float64(entry.Stats.Bytes) / float64(entry.Stats.TotalBytes) * percentHundred)
		}
	}

	return Progress{
		Line:        line,
		Percentage:  percentage,
		Transferred: entry.Stats.Bytes,
		Total:       max(entry.Stats.Bytes, entry.Stats.TotalBytes),
	}, nil
}

func FindLast(text string) Progress {
	return progresslog.FindLast(text, ParseLine)
}
