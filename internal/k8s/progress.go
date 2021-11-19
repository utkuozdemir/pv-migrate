package k8s

import (
	"bufio"
	"context"
	"fmt"
	"github.com/kyokomi/emoji/v2"
	"github.com/schollz/progressbar/v3"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"regexp"
	"strconv"
	"strings"
)

var (
	progressRegex = regexp.MustCompile(`\s*(?P<bytes>[0-9]+(,[0-9]+)*)\s+(?P<percentage>[0-9]{1,3})%`)
	rsyncEndRegex = regexp.MustCompile(`\s*total size is (?P<bytes>[0-9]+(,[0-9]+)*)`)
)

func handlePodLogs(cli kubernetes.Interface, ns string, name string,
	stopCh <-chan bool, showProgressBar bool, logger *log.Entry) {
	if showProgressBar {
		tailPodLogsWithProgressBar(cli, ns, name, stopCh, logger)
	} else {
		tailPodLogsNoProgressBar(cli, ns, name, stopCh, logger)
	}
}

func tailPodLogsNoProgressBar(cli kubernetes.Interface, ns string, name string,
	stopCh <-chan bool, logger *log.Entry) {
	tailPodLogsWithRetry(cli, ns, name, func() {},
		func(s string) { logger.Debug(s) }, func() {}, stopCh, logger)
}

func tailPodLogsWithProgressBar(cli kubernetes.Interface, ns string, name string,
	stopCh <-chan bool, logger *log.Entry) {
	completed := false
	var bar *progressbar.ProgressBar
	tailPodLogsWithRetry(cli, ns, name, func() {
		bar = progressbar.NewOptions64(
			1,
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionFullWidth(),
			progressbar.OptionOnCompletion(func() { fmt.Println() }),
			progressbar.OptionSetDescription(emoji.Sprint(":open_file_folder: Copying data...")),
		)
	}, func(s string) {
		if completed || bar == nil {
			return
		}

		pr, _ := parseLogLine(&s)
		if pr != nil {
			bar.ChangeMax64(pr.total)
			_ = bar.Set64(pr.transferred)
			completed = pr.percentage == 100
		}
	}, func() {
		if bar == nil {
			return
		}

		_ = bar.Finish()
	}, stopCh, logger)
}

// tailPodLogsWithRetry will restart the log tailing if it times out
func tailPodLogsWithRetry(cli kubernetes.Interface, ns string, name string,
	beforeFunc func(), logFunc func(string), successFunc func(),
	stopCh <-chan bool, logger *log.Entry) {
	failedOnce := false
	for {
		done, err := tailPodLogs(cli, ns, name, beforeFunc, logFunc, successFunc, stopCh)
		if err != nil && !failedOnce {
			logger.WithError(err).
				Debug(":large_orange_diamond: Cannot tail logs to display progress")
			failedOnce = true
		}

		if done {
			return
		}
	}
}

func tailPodLogs(cli kubernetes.Interface, ns string, name string, beforeFunc func(),
	logFunc func(string), successFunc func(), stopCh <-chan bool) (bool, error) {
	s, err := cli.CoreV1().Pods(ns).
		GetLogs(name, &corev1.PodLogOptions{Follow: true}).Stream(context.TODO())
	if err != nil {
		return false, err
	}

	defer func() { _ = s.Close() }()

	beforeFunc()
	sc := bufio.NewScanner(s)
	for {
		select {
		case success := <-stopCh:
			if success {
				successFunc()
			}
			return true, nil
		default:
			if !sc.Scan() {
				return false, nil
			}
			logFunc(sc.Text())
		}
	}
}

type progress struct {
	percentage  int
	transferred int64
	total       int64
}

func parseLogLine(l *string) (*progress, error) {
	endMatches := findNamedMatches(rsyncEndRegex, l)
	if len(endMatches) > 0 {
		total, err := parseNumBytes(endMatches["bytes"])
		if err != nil {
			return nil, err
		}
		return &progress{percentage: 100, transferred: total, total: total}, nil
	}

	prMatches := findNamedMatches(progressRegex, l)
	if len(prMatches) == 0 {
		return nil, nil
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
	total := int64((float64(transferred) / float64(percentage)) * 100)

	if transferred > total {
		// in case of a rounding error, update total, since transferred is more accurate
		total = transferred
	}

	return &progress{percentage: percentage, transferred: transferred, total: total}, nil
}

func parseNumBytes(numBytes string) (int64, error) {
	return strconv.ParseInt(strings.Replace(numBytes, ",", "", -1), 10, 64)
}

func findNamedMatches(r *regexp.Regexp, str *string) map[string]string {
	match := r.FindStringSubmatch(*str)
	results := map[string]string{}
	for i, name := range match {
		results[r.SubexpNames()[i]] = name
	}
	return results
}
