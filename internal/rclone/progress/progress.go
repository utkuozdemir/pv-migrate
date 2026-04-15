package progress

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/utkuozdemir/pv-migrate/internal/progresslog"
)

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

	percentage := 0

	if entry.Stats.TotalBytes > 0 {
		if entry.Stats.Bytes >= entry.Stats.TotalBytes {
			percentage = 100
		} else {
			percentage = int(float64(entry.Stats.Bytes) / float64(entry.Stats.TotalBytes) * 100)
		}
	}

	return Progress{
		Line:        line,
		Percentage:  percentage,
		Transferred: entry.Stats.Bytes,
		Total:       entry.Stats.TotalBytes,
	}, nil
}

func FindLast(text string) Progress {
	return progresslog.FindLast(text, ParseLine)
}
