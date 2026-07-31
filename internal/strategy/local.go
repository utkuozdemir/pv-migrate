package strategy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/progresslog"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	rsyncprogress "github.com/utkuozdemir/pv-migrate/internal/rsync/progress"
)

const portForwardTimeout = 30 * time.Second

type Local struct{}

func (r *Local) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
	mig := attempt.Migration
	req := mig.Request

	if req.Detach {
		return Declined("local strategy requires a persistent connection through the local machine")
	}

	if hasHelmOverrides(req) {
		logger.Warn("⚠️  Local strategy does not deploy an rsync Job; " +
			"rsync-related Helm values (e.g. rsync.*) will have no effect")
	}

	publicKey, privateKey, privateKeyMountPath, err := generateSSHKeys(req.KeyAlgorithm, logger)
	if err != nil {
		return err
	}

	srcReleaseName := attempt.HelmReleaseNamePrefix + "-src"
	destReleaseName := attempt.HelmReleaseNamePrefix + "-dest"
	attempt.ReleaseNames = []string{srcReleaseName, destReleaseName}

	if err = installLocalOnSource(
		ctx, attempt, srcReleaseName, publicKey, privateKey, privateKeyMountPath, logger,
	); err != nil {
		return fmt.Errorf("failed to install on source: %w", err)
	}

	if err = installLocalOnDest(ctx, attempt, destReleaseName, publicKey, logger); err != nil {
		return fmt.Errorf("failed to install on dest: %w", err)
	}

	srcPod, err := getSshdPodForHelmRelease(ctx, mig.SourceInfo, srcReleaseName, logger)
	if err != nil {
		return fmt.Errorf("failed to get source sshd pod: %w", err)
	}

	destPod, err := getSshdPodForHelmRelease(ctx, mig.DestInfo, destReleaseName, logger)
	if err != nil {
		return fmt.Errorf("failed to get dest sshd pod: %w", err)
	}

	return runLocalMigration(ctx, attempt, mig, privateKey, srcPod, destPod, sshPort(mig.Request), logger)
}

func runLocalMigration(
	ctx context.Context,
	attempt *migration.Attempt,
	mig *migration.Migration,
	privateKey string,
	srcPod, destPod *corev1.Pod,
	containerSSHPort int,
	logger *slog.Logger,
) error {
	// Size-1 buffers so the port-notifier goroutine inside PortForward can send
	// without blocking, even if the receiver has already taken the ctx.Done() path.
	srcPortCh := make(chan int, 1)
	destPortCh := make(chan int, 1)

	// All three long-running operations share one errgroup so that failure in any one
	// (including a mid-migration port-forward drop) cancels the others via ctx.
	eg, ctx := errgroup.WithContext(ctx)

	// fwdCtx is cancelled either when the errgroup ctx is cancelled (on any error) or
	// when the rsync goroutine finishes (on success), guaranteeing port-forward goroutines
	// always terminate and eg.Wait() never blocks indefinitely.
	fwdCtx, fwdCancel := context.WithCancel(ctx)
	defer fwdCancel()

	eg.Go(func() error {
		return k8s.PortForward(fwdCtx, &k8s.PortForwardRequest{
			RestConfig:   mig.SourceInfo.ClusterClient.RestConfig,
			PodNs:        srcPod.Namespace,
			PodName:      srcPod.Name,
			PodPort:      containerSSHPort,
			ActualPortCh: srcPortCh,
		}, logger)
	})

	eg.Go(func() error {
		return k8s.PortForward(fwdCtx, &k8s.PortForwardRequest{
			RestConfig:   mig.DestInfo.ClusterClient.RestConfig,
			PodNs:        destPod.Namespace,
			PodName:      destPod.Name,
			PodPort:      containerSSHPort,
			ActualPortCh: destPortCh,
		}, logger)
	})

	eg.Go(func() error {
		defer fwdCancel()

		return waitAndRunRsync(ctx, attempt, privateKey, srcPortCh, destPortCh, logger)
	})

	return eg.Wait() //nolint:wrapcheck
}

func hasHelmOverrides(req *migration.Request) bool {
	return len(req.HelmValues) > 0 || len(req.HelmValuesFiles) > 0 ||
		len(req.HelmFileValues) > 0 || len(req.HelmStringValues) > 0
}

func waitAndRunRsync(
	ctx context.Context,
	attempt *migration.Attempt,
	privateKey string,
	srcPortCh, destPortCh <-chan int,
	logger *slog.Logger,
) error {
	// Wait for both port-forwards to be ready before starting rsync.
	timeoutCtx, cancel := context.WithTimeout(ctx, portForwardTimeout)
	defer cancel()

	var srcFwdPort int

	select {
	case <-timeoutCtx.Done():
		return fmt.Errorf("waiting for source port-forward: %w", timeoutCtx.Err())
	case srcFwdPort = <-srcPortCh:
	}

	var destFwdPort int

	select {
	case <-timeoutCtx.Done():
		return fmt.Errorf("waiting for dest port-forward: %w", timeoutCtx.Err())
	case destFwdPort = <-destPortCh:
	}

	return runRsyncOverSSH(ctx, attempt, privateKey, srcFwdPort, destFwdPort, logger)
}

func runRsyncOverSSH(
	ctx context.Context,
	attempt *migration.Attempt,
	privateKey string,
	srcFwdPort, destFwdPort int,
	logger *slog.Logger,
) error {
	signer, err := gossh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	sshConfig := &gossh.ClientConfig{
		User:            sshUser(attempt.Migration.Request),
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
	}

	sshClient, err := gossh.Dial("tcp", fmt.Sprintf("localhost:%d", srcFwdPort), sshConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to source pod: %w", err)
	}

	defer func() { logClose(sshClient, logger, "🔶 Failed to close SSH client") }()

	// Set up reverse tunnel: tunnelPort on the source pod's loopback is forwarded back
	// through this SSH connection to localhost:destFwdPort on the local machine, which
	// in turn port-forwards into the dest pod's sshd.
	tunnelPort := attempt.Migration.Request.SSHReverseTunnelPort

	tunnelListener, err := sshClient.Listen("tcp", fmt.Sprintf("localhost:%d", tunnelPort))
	if err != nil {
		return fmt.Errorf("failed to open reverse tunnel on port %d: %w", tunnelPort, err)
	}

	rsyncCmd, err := buildRsyncCmdLocal(attempt.Migration)
	if err != nil {
		logClose(tunnelListener, logger, "🔶 Failed to close tunnel listener")

		return fmt.Errorf("failed to build rsync command: %w", err)
	}

	session, err := sshClient.NewSession()
	if err != nil {
		logClose(tunnelListener, logger, "🔶 Failed to close tunnel listener")

		return fmt.Errorf("failed to create SSH session: %w", err)
	}

	defer func() { logClose(session, logger, "🔶 Failed to close SSH session") }()

	return runRsyncSession(ctx, session, rsyncCmd, tunnelListener, destFwdPort, attempt.Migration.Request, logger)
}

func runRsyncSession(
	ctx context.Context,
	session *gossh.Session,
	rsyncCmd string,
	tunnelListener net.Listener,
	destFwdPort int,
	req *migration.Request,
	logger *slog.Logger,
) error {
	reader, writer := io.Pipe()

	// The tail records at the source, before the pipe: the async progress
	// pipeline may still be draining when the session ends, but this writer has
	// already seen every byte, so a failure message never misses the last lines.
	tail := &lineTail{limit: sessionTailLines}
	output := io.MultiWriter(tail, writer)

	session.Stdout = output
	session.Stderr = output

	progressLogger := sessionProgressLogger(req, reader)

	// rsyncDone is closed by the rsync goroutine after it finishes (and after its deferred
	// cleanups run), so the context-watcher goroutine knows when to exit.
	rsyncDone := make(chan struct{})

	eg, ctx := errgroup.WithContext(ctx)

	// Context watcher: cancels the SSH session if the context is cancelled externally
	// (e.g. parent timeout), and exits cleanly once rsync is done.
	eg.Go(func() error {
		select {
		case <-ctx.Done():
			logClose(session, logger, "🔶 Failed to close SSH session on cancellation")
		case <-rsyncDone:
		}

		return nil
	})

	// Reverse tunnel forwarder: accepts connections on the configured reverse tunnel port
	// on the source pod's loopback and proxies them to localhost:destFwdPort on the local machine.
	eg.Go(func() error {
		return forwardTunnelConnections(ctx, tunnelListener, destFwdPort, logger)
	})

	// Progress logger: tails the pipe written by the rsync session.
	eg.Go(func() error {
		return progressLogger.Start(ctx, logger)
	})

	// Rsync runner: executes rsync on the source pod via SSH, then tears down shared
	// resources so the other goroutines can exit cleanly.
	var vanished bool

	eg.Go(func() error {
		defer close(rsyncDone)
		defer func() { logClose(writer, logger, "🔶 Failed to close pipe writer") }()
		defer func() { logClose(tunnelListener, logger, "🔶 Failed to close tunnel listener") }()

		var sessionVanished bool

		err := completeRsyncSession(ctx, session.Run(rsyncCmd), progressLogger, &sessionVanished)
		vanished = sessionVanished

		return err
	})

	// The group is joined before anything is reported: the progress bar owns the
	// output until every goroutine has stopped, and the tail is complete by now.
	return finishRsyncSession(eg.Wait(), vanished, tail, progressLogger, logger)
}

// finishRsyncSession reports the joined group's outcome. Only the session's own
// failure gets the exit-status interpretation and the output tail; any other
// error passes through untouched.
func finishRsyncSession(
	waitErr error, vanished bool, tail *lineTail, progressLogger *progresslog.Logger, logger *slog.Logger,
) error {
	if waitErr != nil {
		var sessionErr *rsyncRunError
		if errors.As(waitErr, &sessionErr) {
			return rsyncSessionError(sessionErr.err, tail.Lines())
		}

		return waitErr //nolint:wrapcheck
	}

	progressLogger.FinishBar(logger)

	if vanished {
		logger.Warn("🔶 Completed with a warning: some source files vanished during the transfer and were skipped. " +
			"Re-run the migration, or copy from a source that is not being written to")
	}

	return nil
}

// sessionProgressLogger tails the pipe the rsync session writes into.
func sessionProgressLogger(req *migration.Request, reader io.ReadCloser) *progresslog.Logger {
	return progresslog.NewLogger(progresslog.LoggerOptions{
		Writer:          req.Writer,
		ShowProgressBar: req.ShowProgressBar,
		LogStreamFunc: func(context.Context) (io.ReadCloser, error) {
			return reader, nil
		},
		ParseLineFunc: rsyncprogress.ParseLine,
		Source:        rsyncComponent,
	})
}

// exitStatusError is a failure that carries the status the remote command exited
// with. The interface rather than the concrete type, because the SSH exit error's
// status cannot be set from outside its own package and so cannot be tested for.
type exitStatusError interface {
	error
	ExitStatus() int
}

var _ exitStatusError = (*gossh.ExitError)(nil)

// rsyncRunError marks a failure of the rsync session itself, as opposed to the
// tunnel or the progress logger, so the caller knows which error to explain
// with the session's exit status and output tail.
type rsyncRunError struct {
	err error
}

func (e *rsyncRunError) Error() string { return e.err.Error() }

func (e *rsyncRunError) Unwrap() error { return e.err }

// completeRsyncSession turns the session's result into the attempt's result.
// A vanished-files exit falls through to the completion signal rather than
// returning early: the progress logger keeps retrying the closed pipe until it
// is told the transfer is done, so an early return would never let the group
// finish. The vanished flag is reported back rather than logged here, because
// the progress bar still owns the output at this point.
func completeRsyncSession(
	ctx context.Context,
	runErr error,
	progressLogger *progresslog.Logger,
	vanished *bool,
) error {
	if runErr != nil {
		if !isVanishedSourceFiles(runErr) {
			return &rsyncRunError{err: runErr}
		}

		*vanished = true
	}

	return progressLogger.MarkAsComplete(ctx)
}

// isVanishedSourceFiles reports whether rsync stopped only because files
// disappeared from the source while it was reading them. There is no retry
// script on this path, so the in-cluster script's rule lands here instead.
func isVanishedSourceFiles(runErr error) bool {
	var exitErr exitStatusError

	return errors.As(runErr, &exitErr) && exitErr.ExitStatus() == rsync.VanishedFilesExitCode
}

// rsyncSessionError explains a failed local rsync session with what was observed:
// the status the session reported, rsync's documented meaning for it, and the
// last raw output lines, which unlike an in-cluster job have no log to fetch back.
func rsyncSessionError(runErr error, recentLines []string) error {
	err := fmt.Errorf("rsync session failed: %w", runErr)

	var exitErr exitStatusError
	if errors.As(runErr, &exitErr) {
		if meaning := rsync.Interpret(exitErr.ExitStatus()); meaning != "" {
			err = fmt.Errorf("%w (%s)", err, meaning)
		}
	}

	if len(recentLines) > 0 {
		err = fmt.Errorf("%w\nlast lines of rsync output:\n%s", err, strings.Join(recentLines, "\n"))
	}

	return err
}

func forwardTunnelConnections(ctx context.Context, listener net.Listener, destPort int, logger *slog.Logger) error {
	var eg errgroup.Group

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
				break // listener was closed by the rsync goroutine, normal shutdown
			}

			return fmt.Errorf("tunnel listener accept error: %w", err)
		}

		eg.Go(func() error {
			proxyConn(ctx, conn, destPort, logger)

			return nil
		})
	}

	return eg.Wait() //nolint:wrapcheck
}

func proxyConn(ctx context.Context, src net.Conn, destPort int, logger *slog.Logger) {
	defer func() { logClose(src, logger, "🔶 Failed to close tunnel src connection") }()

	dst, err := (&net.Dialer{}).DialContext(ctx, "tcp", fmt.Sprintf("localhost:%d", destPort))
	if err != nil {
		logger.Debug("tunnel: failed to dial dest", "port", destPort, "error", err)

		return
	}

	defer func() { logClose(dst, logger, "🔶 Failed to close tunnel dst connection") }()

	var eg errgroup.Group

	eg.Go(func() error {
		defer func() { logClose(dst, logger, "🔶 Failed to close tunnel dst connection on copy") }()

		_, copyErr := io.Copy(dst, src)

		return copyErr //nolint:wrapcheck
	})

	eg.Go(func() error {
		defer func() { logClose(src, logger, "🔶 Failed to close tunnel src connection on copy") }()

		_, copyErr := io.Copy(src, dst)

		return copyErr //nolint:wrapcheck
	})

	if copyErr := eg.Wait(); copyErr != nil {
		logger.Debug("tunnel connection closed", "error", copyErr)
	}
}

func buildRsyncCmdLocal(mig *migration.Migration) (string, error) {
	srcPath, destPath, err := resolveMountPaths(mig.Request)
	if err != nil {
		return "", err
	}

	rsyncCmd := rsync.Cmd{
		Port:        mig.Request.SSHReverseTunnelPort,
		NoChown:     mig.Request.NoChown,
		NonRoot:     mig.Request.NonRoot,
		Delete:      mig.Request.DeleteExtraneousFiles,
		SrcPath:     srcPath,
		DestPath:    destPath,
		DestUseSSH:  true,
		DestSSHHost: "localhost",
		DestSSHUser: sshUser(mig.Request),
		Compress:    !mig.Request.NoCompress,
		ExtraArgs:   mig.Request.RsyncExtraArgs,
	}

	cmd, err := rsyncCmd.Build()
	if err != nil {
		return "", fmt.Errorf("failed to build rsync command: %w", err)
	}

	return cmd, nil
}

func getSshdPodForHelmRelease(
	ctx context.Context,
	pvcInfo *pvc.Info,
	name string,
	logger *slog.Logger,
) (*corev1.Pod, error) {
	labelSelector := "app.kubernetes.io/component=sshd,app.kubernetes.io/instance=" + name

	pod, err := k8s.WaitForPod(
		ctx,
		pvcInfo.ClusterClient.KubeClient,
		pvcInfo.Claim.Namespace,
		labelSelector,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get sshd pod for helm release %s: %w", name, err)
	}

	return pod, nil
}

func installLocalOnSource(
	ctx context.Context,
	attempt *migration.Attempt,
	releaseName, publicKey, privateKey, privateKeyMountPath string,
	logger *slog.Logger,
) error {
	mig := attempt.Migration
	side := componentSide{
		info:      mig.SourceInfo,
		mountPath: srcMountPath,
		readOnly:  !mig.Request.SourceMountReadWrite,
	}

	sshdVals := buildSshdHelmValues(side, publicKey)
	sshdVals["privateKeyMount"] = true
	sshdVals["privateKey"] = privateKey
	sshdVals["privateKeyMountPath"] = privateKeyMountPath

	return installHelmChart(ctx, attempt, mig.SourceInfo, releaseName, map[string]any{sshdComponent: sshdVals}, logger)
}

func installLocalOnDest(
	ctx context.Context, attempt *migration.Attempt, releaseName, publicKey string, logger *slog.Logger,
) error {
	mig := attempt.Migration
	side := componentSide{info: mig.DestInfo, mountPath: destMountPath}

	return installHelmChart(
		ctx, attempt, mig.DestInfo, releaseName,
		map[string]any{sshdComponent: buildSshdHelmValues(side, publicKey)}, logger,
	)
}

func logClose(c io.Closer, logger *slog.Logger, msg string) {
	if err := c.Close(); err != nil {
		// EOF and "use of closed network connection" mean the other side already
		// closed the connection before our deferred close ran — expected on clean exit.
		if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
			logger.Debug(msg, "error", err)

			return
		}

		logger.Warn(msg, "error", err)
	}
}
