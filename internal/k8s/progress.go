package k8s

import (
	"context"
	"fmt"
	"github.com/schollz/progressbar/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	progressRegex = regexp.MustCompile(`\s*(?P<bytes>[0-9]+(,[0-9]+)*)\s+(?P<percentage>[0-9]{1,3})%`)
	rsyncEndRegex = regexp.MustCompile(`\s*total size is (?P<bytes>[0-9]+(,[0-9]+)*)`)
)

func tailLogsForProgress(kubeClient kubernetes.Interface, namespace string, pod string) error {
	defer fmt.Println()
	bar := progressbar.NewOptions64(
		1,
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionFullWidth(),
	)
	var since metav1.Time
	for {
		logs, err := getLogs(kubeClient, &namespace, &pod, &since)
		if err != nil {
			return err
		}

		pr, err := getLatestProgress(logs)
		if err != nil {
			return err
		}

		if pr != nil {
			bar.ChangeMax64(pr.total)
			err = bar.Set64(pr.transferred)
			if err != nil {
				return err
			}
			if pr.percentage == 100 {
				return nil
			}
		}

		since = metav1.Now()
		time.Sleep(1 * time.Second)
	}
}

func getLatestProgress(logs []string) (*progress, error) {
	for i := len(logs) - 1; i >= 0; i-- {
		l := logs[i]
		pr, err := parseLogLine(&l)
		if err != nil {
			return nil, err
		}

		if pr != nil {
			return pr, nil
		}
	}
	return nil, nil
}

func getLogs(kubeClient kubernetes.Interface, namespace *string,
	pod *string, since *metav1.Time) ([]string, error) {
	podLogOptions := corev1.PodLogOptions{
		SinceTime: since,
	}

	podLogRequest := kubeClient.CoreV1().Pods(*namespace).GetLogs(*pod, &podLogOptions)
	bytes, err := podLogRequest.DoRaw(context.TODO())
	if err != nil {
		return nil, err
	}
	return strings.Split(string(bytes), "\n"), nil
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
