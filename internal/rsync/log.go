package rsync

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/kyokomi/emoji/v2"
	"github.com/schollz/progressbar/v3"
	log "github.com/sirupsen/logrus"
)

var (
	progressRegex = regexp.MustCompile(`\s*(?P<bytes>[0-9]+(,[0-9]+)*)\s+(?P<percentage>[0-9]{1,3})%`)
	rsyncEndRegex = regexp.MustCompile(`\s*total size is (?P<bytes>[0-9]+(,[0-9]+)*)`)

	errNoMatch = errors.New("no match")
)

const (
	percentHundred = 100

	bytesTransferredIntBase   = 10
	bytesTransferredInt64Bits = 64
)

type LogTail struct {
	LogReaderFunc   func() (io.ReadCloser, error)
	SuccessCh       <-chan bool
	ShowProgressBar bool
	Logger          *log.Entry
}

type progress struct {
	percentage  int
	transferred int64
	total       int64
}

func (l *LogTail) Start() {
	if l.ShowProgressBar {
		l.tailWithProgressBar()

		return
	}
	l.tailNoProgressBar()
}

func (l *LogTail) tailNoProgressBar() {
	l.tailWithRetry(func() {}, func(s string) { l.Logger.Debug(s) }, func() {})
}

func (l *LogTail) tailWithProgressBar() {
	completed := false
	var bar *progressbar.ProgressBar
	l.tailWithRetry(func() {
		bar = progressbar.NewOptions64(
			1,
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionFullWidth(),
			progressbar.OptionOnCompletion(func() { fmt.Println() }), //nolint:forbidigo
			progressbar.OptionSetDescription(emoji.Sprint(":open_file_folder: Copying data...")),
		)
	}, func(s string) {
		if completed || bar == nil {
			return
		}

		pr, _ := parseLine(&s)
		if pr != nil {
			bar.ChangeMax64(pr.total)
			_ = bar.Set64(pr.transferred)
			completed = pr.percentage == percentHundred
		}
	}, func() {
		if bar == nil {
			return
		}

		_ = bar.Finish()
	})
}

// tailWithRetry will restart the log tailing if it times out.
func (l *LogTail) tailWithRetry(beforeFunc func(), logFunc func(string), successFunc func()) {
	failedOnce := false
	for {
		done, err := l.tail(beforeFunc, logFunc, successFunc)
		if err != nil && !failedOnce {
			l.Logger.WithError(err).
				Debug(":large_orange_diamond: Cannot tail logs to display progress")
			failedOnce = true
		}

		if done {
			return
		}
	}
}

func (l *LogTail) tail(beforeFunc func(),
	logFunc func(string), successFunc func(),
) (bool, error) {
	logReader, err := l.LogReaderFunc()
	if err != nil {
		return false, err
	}

	defer func() { _ = logReader.Close() }()

	beforeFunc()
	scanner := bufio.NewScanner(logReader)
	for {
		select {
		case success := <-l.SuccessCh:
			if success {
				successFunc()
			}

			return true, nil
		default:
			if !scanner.Scan() {
				return false, nil
			}
			logFunc(scanner.Text())
		}
	}
}

func parseLine(line *string) (*progress, error) {
	endMatches := findNamedMatches(rsyncEndRegex, line)
	if len(endMatches) > 0 {
		total, err := parseNumBytes(endMatches["bytes"])
		if err != nil {
			return nil, err
		}

		return &progress{percentage: percentHundred, transferred: total, total: total}, nil
	}

	prMatches := findNamedMatches(progressRegex, line)
	if len(prMatches) == 0 {
		return nil, errNoMatch
	}

	percentage, err := strconv.Atoi(prMatches["percentage"])
	if err != nil {
		return nil, err
	}

	if percentage == 0 {
		// avoid division by zero but allow estimating a total number
		percentage = 1
	}

	transferred, err := parseNumBytes(prMatches["bytes"])
	if err != nil {
		return nil, err
	}
	total := int64((float64(transferred) / float64(percentage)) * percentHundred)

	if transferred > total {
		// in case of a rounding error, update total, since transferred is more accurate
		total = transferred
	}

	return &progress{percentage: percentage, transferred: transferred, total: total}, nil
}

func parseNumBytes(numBytes string) (int64, error) {
	return strconv.ParseInt(strings.ReplaceAll(numBytes, ",", ""),
		bytesTransferredIntBase, bytesTransferredInt64Bits)
}

func findNamedMatches(r *regexp.Regexp, str *string) map[string]string {
	match := r.FindStringSubmatch(*str)
	results := map[string]string{}
	for i, name := range match {
		results[r.SubexpNames()[i]] = name
	}

	return results
}
