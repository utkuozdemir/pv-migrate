package strategy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/rsync/progress"
	internalssh "github.com/utkuozdemir/pv-migrate/internal/ssh"
)

const portForwardTimeout = 30 * time.Second

type Local struct{}

func (r *Local) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
	mig := attempt.Migration
	req := mig.Request

	if hasHelmOverrides(req) {
		logger.Warn("⚠️  Local strategy does not deploy an rsync Job; " +
			"rsync-related Helm values (e.g. rsync.*) will have no effect")
	}

	keyAlgorithm := req.KeyAlgorithm

	logger.Info("🔑 Generating SSH key pair", "algorithm", keyAlgorithm)

	publicKey, privateKey, err := internalssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return fmt.Errorf("failed to generate SSH key pair: %w", err)
	}

	privateKeyMountPath := "/tmp/id_" + keyAlgorithm
	srcReleaseName := attempt.HelmReleaseNamePrefix + "-src"
	destReleaseName := attempt.HelmReleaseNamePrefix + "-dest"
	releaseNames := []string{srcReleaseName, destReleaseName}

	// Register cleanup hook before installing any charts so that if only the source
	// install succeeds, it is still cleaned up on failure or exit.
	doneCh := registerCleanupHook(attempt, releaseNames, logger)
	defer cleanupAndReleaseHook(ctx, attempt, releaseNames, doneCh, logger)

	if err = installLocalOnSource(
		attempt, srcReleaseName, publicKey, privateKey, privateKeyMountPath, srcMountPath, logger,
	); err != nil {
		return fmt.Errorf("failed to install on source: %w", err)
	}

	if err = installLocalOnDest(attempt, destReleaseName, publicKey, destMountPath, logger); err != nil {
		return fmt.Errorf("failed to install on dest: %w", err)
	}

	srcPod, err := getSshdPodForHelmRelease(ctx, mig.SourceInfo, srcReleaseName)
	if err != nil {
		return fmt.Errorf("failed to get source sshd pod: %w", err)
	}

	destPod, err := getSshdPodForHelmRelease(ctx, mig.DestInfo, destReleaseName)
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

	session.Stdout = writer
	session.Stderr = writer

	progressLogger := progress.NewLogger(progress.LoggerOptions{
		Writer:          req.Writer,
		ShowProgressBar: req.ShowProgressBar,
		LogStreamFunc: func(context.Context) (io.ReadCloser, error) {
			return reader, nil
		},
	})

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
	eg.Go(func() error {
		defer close(rsyncDone)
		defer func() { logClose(writer, logger, "🔶 Failed to close pipe writer") }()
		defer func() { logClose(tunnelListener, logger, "🔶 Failed to close tunnel listener") }()

		if runErr := session.Run(rsyncCmd); runErr != nil {
			return fmt.Errorf("rsync session failed: %w", runErr)
		}

		return progressLogger.MarkAsComplete(ctx)
	})

	return eg.Wait() //nolint:wrapcheck
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
	srcPath := srcMountPath + "/" + mig.Request.Source.Path
	destPath := destMountPath + "/" + mig.Request.Dest.Path

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
) (*corev1.Pod, error) {
	labelSelector := "app.kubernetes.io/component=sshd,app.kubernetes.io/instance=" + name

	pod, err := k8s.WaitForPod(
		ctx,
		pvcInfo.ClusterClient.KubeClient,
		pvcInfo.Claim.Namespace,
		labelSelector,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get sshd pod for helm release %s: %w", name, err)
	}

	return pod, nil
}

func installLocalOnSource(
	attempt *migration.Attempt,
	releaseName, publicKey, privateKey, privateKeyMountPath, srcMountPath string,
	logger *slog.Logger,
) error {
	mig := attempt.Migration
	sourceInfo := mig.SourceInfo
	namespace := sourceInfo.Claim.Namespace

	vals := map[string]any{
		"sshd": map[string]any{
			"enabled":             true,
			"namespace":           namespace,
			"publicKey":           publicKey,
			"privateKeyMount":     true,
			"privateKey":          privateKey,
			"privateKeyMountPath": privateKeyMountPath,
			"pvcMounts": []map[string]any{
				{
					"name":      sourceInfo.Claim.Name,
					"readOnly":  !mig.Request.SourceMountReadWrite,
					"mountPath": srcMountPath,
				},
			},
			"affinity": sourceInfo.AffinityHelmValues,
		},
	}

	return installHelmChart(attempt, sourceInfo, releaseName, vals, logger)
}

func installLocalOnDest(
	attempt *migration.Attempt,
	releaseName, publicKey, destMountPath string,
	logger *slog.Logger,
) error {
	mig := attempt.Migration
	destInfo := mig.DestInfo
	namespace := destInfo.Claim.Namespace

	vals := map[string]any{
		"sshd": map[string]any{
			"enabled":   true,
			"namespace": namespace,
			"publicKey": publicKey,
			"pvcMounts": []map[string]any{
				{
					"name":      destInfo.Claim.Name,
					"mountPath": destMountPath,
				},
			},
			"affinity": destInfo.AffinityHelmValues,
		},
	}

	return installHelmChart(attempt, destInfo, releaseName, vals, logger)
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
