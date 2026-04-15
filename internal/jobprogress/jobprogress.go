package jobprogress

import (
	"strings"

	"github.com/utkuozdemir/pv-migrate/internal/progresslog"
	rcloneprogress "github.com/utkuozdemir/pv-migrate/internal/rclone/progress"
	rsyncprogress "github.com/utkuozdemir/pv-migrate/internal/rsync/progress"
)

const (
	rsyncSuffix  = "-rsync"
	rcloneSuffix = "-rclone"
)

func Description(jobName string) string {
	switch {
	case strings.HasSuffix(jobName, rsyncSuffix):
		return "rsync"
	case strings.HasSuffix(jobName, rcloneSuffix):
		return "rclone"
	default:
		return "job"
	}
}

func NewLogger(jobName string, options progresslog.LoggerOptions) *progresslog.Logger {
	switch {
	case strings.HasSuffix(jobName, rsyncSuffix):
		options.ParseLineFunc = rsyncprogress.ParseLine
		options.Source = "rsync"
	case strings.HasSuffix(jobName, rcloneSuffix):
		options.ParseLineFunc = rcloneprogress.ParseLine
		options.Source = "rclone"
	}

	return progresslog.NewLogger(options)
}

func FindLast(jobName, text string) (progresslog.Update, bool) {
	switch {
	case strings.HasSuffix(jobName, rsyncSuffix):
		return rsyncprogress.FindLast(text), true
	case strings.HasSuffix(jobName, rcloneSuffix):
		return rcloneprogress.FindLast(text), true
	default:
		return progresslog.Update{}, false
	}
}
