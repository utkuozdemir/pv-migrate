package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/internal/console"
	"github.com/utkuozdemir/pv-migrate/internal/jobprogress"
	"github.com/utkuozdemir/pv-migrate/internal/progresslog"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
)

const jobLogTailLines = 100

// FindJobPod returns a pod for the given job, preferring a Running pod.
func FindJobPod(ctx context.Context, cli kubernetes.Interface, job *batchv1.Job) (*corev1.Pod, error) {
	pods, err := cli.CoreV1().Pods(job.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + job.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods for job %s: %w", job.Name, err)
	}

	for i := range pods.Items {
		if pods.Items[i].Status.Phase == corev1.PodRunning {
			return &pods.Items[i], nil
		}
	}

	if len(pods.Items) > 0 {
		return &pods.Items[0], nil
	}

	return nil, fmt.Errorf("no pods found for job %s", job.Name)
}

// The suffixes the pv-migrate Helm chart gives its job names, which is also how
// the data mover behind a job is identified.
const (
	rsyncJobSuffix  = "-rsync"
	rcloneJobSuffix = "-rclone"
)

var jobSuffixes = []string{rsyncJobSuffix, rcloneJobSuffix}

// FindDataMoverJob finds the data-mover job (rsync or rclone) for a migration by listing
// all Helm-managed jobs and matching by the release name prefix plus a known suffix.
// If nothing is found in the given namespace, it retries across all namespaces.
func FindDataMoverJob(
	ctx context.Context,
	cli kubernetes.Interface,
	ns, releasePrefix string,
	logger *slog.Logger,
) (*batchv1.Job, error) {
	jobs, err := cli.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=Helm",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	for i := range jobs.Items {
		job := &jobs.Items[i]
		if !strings.HasPrefix(job.Name, releasePrefix) {
			continue
		}

		for _, suffix := range jobSuffixes {
			if strings.HasSuffix(job.Name, suffix) {
				return job, nil
			}
		}
	}

	if ns != "" {
		if logger != nil {
			logger.Warn("No data-mover job found in namespace, retrying across all namespaces",
				"namespace", ns, "release_prefix", releasePrefix)
		}

		return FindDataMoverJob(ctx, cli, "", releasePrefix, logger)
	}

	return nil, fmt.Errorf("no job found for migration %s", releasePrefix)
}

// WaitForJobStart waits until the job's pod transitions out of the Pending phase.
// It returns the pod object once the pod is running (or has already terminated).
func WaitForJobStart(ctx context.Context, cli kubernetes.Interface,
	ns, name string, logger *slog.Logger,
) (*corev1.Pod, error) {
	labelSelector := "job-name=" + name

	logger.Info("⏳ Waiting for job pod to start", "job", name)

	pod, err := WaitForPod(ctx, cli, ns, labelSelector, logger)
	if err != nil {
		return nil, err
	}

	switch pod.Status.Phase { //nolint:exhaustive
	case corev1.PodRunning:
		logger.Info("🏃 Job pod is running", "pod", pod.Name)
	case corev1.PodSucceeded:
		logger.Info("🏁 Job pod has already completed", "pod", pod.Name)
	case corev1.PodFailed:
		// Not "completed": on a skim that reads as success.
		logger.Warn("🔶 Job pod already terminated with a failure", "pod", pod.Name)
	default:
		logger.Info("✅ Job pod has started", "pod", pod.Name, "phase", pod.Status.Phase)
	}

	return pod, nil
}

// WaitForJobCompletion waits for the Kubernetes job to complete. With
// structuredLogs set, everything that would be raw text on the writer travels
// as log records instead, so a machine-readable stream stays machine-readable.
//
// showBar reports whether a progress bar belongs on this run. Never on a
// structured stream: a bar paints with carriage returns and escape sequences,
// which have no business in output something else is meant to parse. Decided
// here, the one place every caller passes through, rather than by each of them.
func showBar(requested, structuredLogs bool) bool {
	return requested && !structuredLogs
}

//nolint:funlen
func WaitForJobCompletion(ctx context.Context, cli kubernetes.Interface,
	ns, name string, showProgressBar, structuredLogs bool, palette console.Palette,
	writer io.Writer, logger *slog.Logger,
) (retErr error) {
	if writer == nil {
		writer = io.Discard
	}

	showProgressBar = showBar(showProgressBar, structuredLogs)

	pod, handled, err := resolveRunningJobPod(ctx, cli, ns, name, structuredLogs, palette, writer, logger)
	if handled {
		return err
	}

	tailCtx, tailCancel := context.WithCancel(ctx)
	defer tailCancel()

	var (
		eg      errgroup.Group
		stopped bool
		// The bar overwrites one unterminated line until Finish prints its
		// newline; whoever stops the follower before that has to terminate it.
		barOpen = showProgressBar
		// The follower retries an ended stream, and a retried follow replays
		// the whole log from the start, which would re-drive the progress bar
		// to 100% once per retry. Progress lines are cumulative, so a resumed
		// stream only needs the lines it has not seen. Only the follower's own
		// goroutine calls this, so the counter needs no locking.
		streamedOnce = false
		progress     = jobprogress.NewLogger(name, progresslog.LoggerOptions{
			Writer:          writer,
			ShowProgressBar: showProgressBar,
			LogStreamFunc: func(ctx context.Context) (io.ReadCloser, error) {
				options := followLogOptions(streamedOnce)
				streamedOnce = true

				return cli.CoreV1().Pods(ns).GetLogs(pod.Name, options).Stream(ctx)
			},
		})
	)

	// stopFollower cancels the log tail and joins its goroutine. The follower
	// retries an ended stream until it is cancelled, and it owns the writer
	// until it has stopped, so nothing else may print before this returns. It
	// reports its error once, to whoever stops it first.
	stopFollower := func() error {
		if stopped {
			return nil
		}

		stopped = true

		tailCancel()

		followErr := eg.Wait()

		if barOpen && progress.Rendered() {
			fmt.Fprintln(writer)
		}

		return followErr
	}

	defer func() { retErr = errors.Join(retErr, stopFollower()) }()

	eg.Go(func() error {
		return progress.Start(tailCtx, logger)
	})

	// A new variable on purpose: the follower's stream closure above reads pod,
	// and reassigning it here would race with a live goroutine.
	finalPod, err := waitForPodTermination(ctx, cli, pod.Namespace, pod.Name)
	if err != nil {
		return err
	}

	if finalPod.Status.Phase != corev1.PodSucceeded {
		followerErr := stopFollower()

		writeFailureTail(ctx, cli, name, finalPod, structuredLogs, palette, writer, logger)

		return errors.Join(failedPodError(ctx, cli, name, finalPod), followerErr)
	}

	if err = progress.MarkAsComplete(ctx); err != nil {
		return fmt.Errorf("failed to mark progress logger as complete: %w", err)
	}

	// The bar is finished explicitly below, once the follower has stopped, so
	// the stop must not terminate the line first.
	barOpen = false

	// Join the follower before logging anything else; it owns the writer until
	// it has fully stopped, and the warning below must not interleave with it.
	if followerErr := stopFollower(); followerErr != nil {
		return followerErr
	}

	progress.FinishBar(logger)

	// The progress parser consumed the log, so the marker the job script prints
	// for skipped files has to be fetched back to be seen at all.
	warnIfSourceFilesVanished(recentPodLogs(ctx, cli, finalPod, logger), logger)

	return nil
}

// followLogOptions returns the follow options for the job pod's log stream: the
// whole log on the first open, only new lines on a retry, so a replay cannot
// rewind and re-complete the progress state.
func followLogOptions(alreadyStreamed bool) *corev1.PodLogOptions {
	options := &corev1.PodLogOptions{Follow: true}

	if alreadyStreamed {
		tail := int64(0)
		options.TailLines = &tail
	}

	return options
}

// resolveRunningJobPod resolves everything that can be answered without a log
// follower: a pod that is already terminal, a finished job with no pods left,
// or a fresh pod that died before running. It reports handled=true when the
// wait is over, with the error to return; otherwise the returned pod is
// running and ready to be followed.
func resolveRunningJobPod(
	ctx context.Context,
	cli kubernetes.Interface,
	ns, name string,
	structuredLogs bool,
	palette console.Palette,
	writer io.Writer,
	logger *slog.Logger,
) (*corev1.Pod, bool, error) {
	pod, anyPods, err := findTerminalJobPod(ctx, cli, ns, name)
	if err != nil {
		return nil, true, err
	}

	if pod != nil {
		return nil, true, finishTerminalPod(ctx, cli, name, pod, structuredLogs, palette, writer, logger)
	}

	if !anyPods {
		// A finished job whose pods are gone (deleted mid-run, drained node, TTL
		// cleanup) has nothing to wait for: waiting for a pod would block for the
		// whole watch timeout and then explain nothing.
		if done, jobErr := finishedJobWithoutPods(ctx, cli, ns, name); done {
			return nil, true, jobErr
		}
	}

	pod, err = WaitForJobStart(ctx, cli, ns, name, logger)
	if err != nil {
		return nil, true, err
	}

	if pod.Status.Phase != corev1.PodRunning {
		return nil, true, finishTerminalPod(ctx, cli, name, pod, structuredLogs, palette, writer, logger)
	}

	return pod, false, nil
}

// finishedJobWithoutPods reports whether the job already finished with no pods
// left to inspect, and the error to return for it. An unreadable job falls
// through to the normal waiting path.
func finishedJobWithoutPods(ctx context.Context, cli kubernetes.Interface, ns, name string) (bool, error) {
	job, err := cli.BatchV1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, nil //nolint:nilerr // fall through to waiting for a pod
	}

	if job.Status.Succeeded > 0 {
		return true, nil
	}

	if job.Status.Failed > 0 {
		detail := ""

		for i := range job.Status.Conditions {
			cond := &job.Status.Conditions[i]
			if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue && cond.Message != "" {
				detail = ": " + cond.Message

				break
			}
		}

		return true, fmt.Errorf("job %s/%s failed and its pod is gone%s", ns, name, detail)
	}

	return false, nil
}

// finishTerminalPod handles a job pod that was already done when it was found.
// There is no follower running here, so the log tail can go straight out.
func finishTerminalPod(
	ctx context.Context,
	cli kubernetes.Interface,
	jobName string,
	pod *corev1.Pod,
	structuredLogs bool,
	palette console.Palette,
	writer io.Writer,
	logger *slog.Logger,
) error {
	if pod.Status.Phase != corev1.PodSucceeded {
		writeFailureTail(ctx, cli, jobName, pod, structuredLogs, palette, writer, logger)

		return failedPodError(ctx, cli, jobName, pod)
	}

	tail := recentPodLogs(ctx, cli, pod, logger)
	if !structuredLogs {
		writeTail(pod, jobName, tail, palette, writer, logger)
	}

	warnIfSourceFilesVanished(tail, logger)

	return nil
}

// writeFailureTail hands over the failed pod's last log lines. On a structured
// stream the raw lines would corrupt the records around them, so they travel
// inside a record instead of on the writer.
func writeFailureTail(
	ctx context.Context,
	cli kubernetes.Interface,
	jobName string,
	pod *corev1.Pod,
	structuredLogs bool,
	palette console.Palette,
	writer io.Writer,
	logger *slog.Logger,
) {
	tail := recentPodLogs(ctx, cli, pod, logger)
	if tail == "" {
		return
	}

	if structuredLogs {
		logger.Warn("🗒 Last log lines of the failed job pod", "pod", pod.Namespace+"/"+pod.Name, "tail", tail)

		return
	}

	writeTail(pod, jobName, tail, palette, writer, logger)
}

func findTerminalJobPod(
	ctx context.Context,
	cli kubernetes.Interface,
	ns, jobName string,
) (*corev1.Pod, bool, error) {
	pods, err := cli.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to list pods for job %s: %w", jobName, err)
	}

	var terminal *corev1.Pod

	for idx := range pods.Items {
		switch pods.Items[idx].Status.Phase { //nolint:exhaustive
		case corev1.PodPending, corev1.PodRunning:
			return nil, true, nil
		case corev1.PodSucceeded:
			terminal = &pods.Items[idx]
		case corev1.PodFailed:
			if terminal == nil {
				terminal = &pods.Items[idx]
			}
		}
	}

	return terminal, len(pods.Items) > 0, nil
}

// warnIfSourceFilesVanished surfaces the marker as a single line. It matters most
// with --delete, where the files rsync skipped keep their old copies on the
// destination while the run reports success. The count comes from rsync's own
// vanished-file lines in the tail, so it is observed, not inferred, and it can
// be missing when those lines scrolled out of the tail.
func warnIfSourceFilesVanished(tail string, logger *slog.Logger) {
	if !strings.Contains(tail, rsync.VanishedFilesMarker) {
		return
	}

	const msg = "🔶 Completed with a warning: some source files vanished during the transfer and were skipped. " +
		"Re-run the migration, or copy from a source that is not being written to"

	if count := strings.Count(tail, "file has vanished:"); count > 0 {
		logger.Warn(msg, "vanished_files", count)

		return
	}

	logger.Warn(msg)
}

// writeTail puts a fetched log tail on the writer as a labelled, indented
// quotation, so a reader can tell where the tool stops talking and the pod's
// own output starts.
func writeTail(pod *corev1.Pod, jobName, tail string, palette console.Palette, writer io.Writer, logger *slog.Logger) {
	if tail == "" || writer == nil {
		return
	}

	var rendered strings.Builder

	fmt.Fprintf(&rendered, "\n%s\n",
		palette.Dim(fmt.Sprintf("Last log lines of pod %s/%s:", pod.Namespace, pod.Name)))

	for line := range strings.SplitSeq(strings.TrimRight(renderTail(jobName, tail), "\n"), "\n") {
		rendered.WriteString("  " + line + "\n")
	}

	rendered.WriteString("\n")

	if _, err := io.WriteString(writer, rendered.String()); err != nil {
		logger.Debug("failed to write terminal job pod logs", "pod", pod.Namespace+"/"+pod.Name, "error", err)
	}
}

// renderTail makes a tail readable without dropping evidence. rclone writes
// structured JSON records whose useful part is the level and message, so those
// are extracted; then, for every mover, lines that repeat anywhere in the tail
// are printed once with an explicit count, the same convention the event lines
// use. Counting instead of dropping keeps the collapse lossless.
func renderTail(jobName, tail string) string {
	lines := strings.Split(strings.TrimRight(tail, "\n"), "\n")

	if strings.HasSuffix(jobName, rcloneJobSuffix) {
		mapRcloneRecords(lines)
	}

	counts := make(map[string]int, len(lines))
	for _, line := range lines {
		counts[line]++
	}

	out := make([]string, 0, len(lines))
	seen := make(map[string]bool, len(lines))

	for _, line := range lines {
		if seen[line] {
			continue
		}

		seen[line] = true

		if repeat := counts[line]; repeat > 1 && strings.TrimSpace(line) != "" {
			line += fmt.Sprintf(" (x%d)", repeat)
		}

		out = append(out, line)
	}

	return strings.Join(out, "\n") + "\n"
}

// mapRcloneRecords rewrites rclone's structured JSON records in place as
// "level: message" lines; whatever does not parse is kept as it came.
func mapRcloneRecords(lines []string) {
	for idx, line := range lines {
		var record struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		}

		if err := json.Unmarshal([]byte(line), &record); err != nil || record.Msg == "" {
			continue
		}

		if record.Level != "" {
			lines[idx] = record.Level + ": " + record.Msg
		} else {
			lines[idx] = record.Msg
		}
	}
}

// podLogFetchTimeout bounds the evidence fetch on paths where the parent
// context may already be over, which is often the very failure being explained.
const podLogFetchTimeout = 10 * time.Second

// recentPodLogs returns a bounded tail of the pod's log, or an empty string when
// it cannot be read. The log is evidence, not something to fail over, and it is
// fetched even when the parent context was cancelled, since a timeout is one of
// the failures the tail is fetched to explain.
func recentPodLogs(
	ctx context.Context,
	cli kubernetes.Interface,
	pod *corev1.Pod,
	logger *slog.Logger,
) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), podLogFetchTimeout)
	defer cancel()

	tailLines := int64(jobLogTailLines)

	stream, err := cli.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name,
		&corev1.PodLogOptions{TailLines: &tailLines}).Stream(ctx)
	if err != nil {
		logger.Debug("failed to read terminal job pod logs", "pod", pod.Namespace+"/"+pod.Name, "error", err)

		return ""
	}

	defer func() {
		if closeErr := stream.Close(); closeErr != nil {
			logger.Debug("failed to close terminal job pod log stream", "pod", pod.Namespace+"/"+pod.Name,
				"error", closeErr)
		}
	}()

	data, err := io.ReadAll(stream)
	if err != nil {
		logger.Debug("failed to read terminal job pod logs", "pod", pod.Namespace+"/"+pod.Name, "error", err)

		return ""
	}

	// rsync redraws its progress line with bare carriage returns; normalized
	// here so every consumer of the tail sees plain lines.
	tail := strings.ReplaceAll(string(data), "\r\n", "\n")

	return strings.ReplaceAll(tail, "\r", "\n")
}
