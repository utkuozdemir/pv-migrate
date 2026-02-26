package strategy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/rsync/progress"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
)

const (
	portForwardTimeout   = 30 * time.Second
	sshReverseTunnelPort = 50000

	localSSHPort       = 22
	privateKeyFileMode = 0o600
)

type Local struct{}

func (r *Local) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
	_, err := exec.LookPath("ssh")
	if err != nil {
		return errors.New("ssh binary not found")
	}

	mig := attempt.Migration
	sourceInfo := mig.SourceInfo
	destInfo := mig.DestInfo

	srcReleaseName, destReleaseName, privateKey, err := r.installLocalReleases(attempt, logger)
	if err != nil {
		return fmt.Errorf("failed to install local releases: %w", err)
	}

	releaseNames := []string{srcReleaseName, destReleaseName}

	doneCh := registerCleanupHook(attempt, releaseNames, logger)
	defer cleanupAndReleaseHook(ctx, attempt, releaseNames, doneCh, logger)

	srcFwdPort, srcStopChan, err := portForwardToSshd(ctx, sourceInfo, srcReleaseName, logger)
	if err != nil {
		return fmt.Errorf("failed to port-forward to source: %w", err)
	}

	defer func() { srcStopChan <- struct{}{} }()

	destFwdPort, destStopChan, err := portForwardToSshd(ctx, destInfo, destReleaseName, logger)
	if err != nil {
		return fmt.Errorf("failed to port-forward to destination: %w", err)
	}

	defer func() { destStopChan <- struct{}{} }()

	privateKeyFile, err := writePrivateKeyToTempFile(privateKey)
	if err != nil {
		return fmt.Errorf("failed to write private key to temp file: %w", err)
	}

	defer func() {
		os.Remove(privateKeyFile)
	}()

	rsyncCmd, err := buildRsyncCmdLocal(mig)
	if err != nil {
		return fmt.Errorf("failed to build rsync command: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ssh", "-i", privateKeyFile,
		"-p", strconv.Itoa(srcFwdPort),
		"-R", fmt.Sprintf("%d:localhost:%d", sshReverseTunnelPort, destFwdPort),
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "root@localhost",
		rsyncCmd,
	)

	if err = runCmdLocal(ctx, attempt, cmd, logger); err != nil {
		return fmt.Errorf("failed to run rsync command: %w", err)
	}

	return nil
}

func runCmdLocal(
	ctx context.Context,
	attempt *migration.Attempt,
	cmd *exec.Cmd,
	logger *slog.Logger,
) (retErr error) {
	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer

	errorCh := make(chan error)

	//nolint:godox
	go func() { errorCh <- cmd.Run() }() // todo: this is a mess, refactor

	showProgressBar := attempt.Migration.Request.ShowProgressBar

	progressLogger := progress.NewLogger(progress.LoggerOptions{
		Writer:          attempt.Migration.Request.Writer,
		ShowProgressBar: showProgressBar,
		LogStreamFunc: func(context.Context) (io.ReadCloser, error) {
			return reader, nil
		},
	})

	tailCtx, tailCancel := context.WithCancel(ctx)
	defer tailCancel()

	var eg errgroup.Group

	eg.Go(func() error {
		return progressLogger.Start(tailCtx, logger)
	})

	defer func() {
		retErr = errors.Join(retErr, eg.Wait())
	}()

	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck
	case err := <-errorCh:
		if err == nil {
			if finishErr := progressLogger.MarkAsComplete(ctx); finishErr != nil {
				return fmt.Errorf("failed to mark progress logger as complete: %w", finishErr)
			}
		}

		return err
	}
}

func buildRsyncCmdLocal(mig *migration.Migration) (string, error) {
	srcPath := srcMountPath + "/" + mig.Request.Source.Path
	destPath := destMountPath + "/" + mig.Request.Dest.Path

	rsyncCmd := rsync.Cmd{
		Port:        sshReverseTunnelPort,
		NoChown:     mig.Request.NoChown,
		Delete:      mig.Request.DeleteExtraneousFiles,
		SrcPath:     srcPath,
		DestPath:    destPath,
		DestUseSSH:  true,
		DestSSHHost: "localhost",
		Compress:    mig.Request.Compress,
	}

	cmd, err := rsyncCmd.Build()
	if err != nil {
		return "", fmt.Errorf("failed to build rsync command: %w", err)
	}

	return cmd, nil
}

func (r *Local) installLocalReleases(
	attempt *migration.Attempt,
	logger *slog.Logger,
) (string, string, string, error) {
	keyAlgorithm := attempt.Migration.Request.KeyAlgorithm

	logger.Info("🔑 Generating SSH key pair", "algorithm", keyAlgorithm)

	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate SSH key pair: %w", err)
	}

	privateKeyMountPath := "/tmp/id_" + keyAlgorithm

	srcReleaseName := attempt.HelmReleaseNamePrefix + "-src"
	destReleaseName := attempt.HelmReleaseNamePrefix + "-dest"

	err = installLocalOnSource(attempt, srcReleaseName, publicKey, privateKey, privateKeyMountPath, srcMountPath)
	if err != nil {
		return "", "", "", err
	}

	err = installLocalOnDest(attempt, destReleaseName, publicKey, destMountPath)
	if err != nil {
		return "", "", "", err
	}

	return srcReleaseName, destReleaseName, privateKey, nil
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

func installLocalOnSource(attempt *migration.Attempt, releaseName,
	publicKey, privateKey, privateKeyMountPath, srcMountPath string,
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
					"readOnly":  mig.Request.SourceMountReadOnly,
					"mountPath": srcMountPath,
				},
			},
			"affinity": sourceInfo.AffinityHelmValues,
		},
	}

	return installHelmChart(attempt, sourceInfo, releaseName, vals)
}

func installLocalOnDest(attempt *migration.Attempt, releaseName, publicKey, destMountPath string) error {
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

	valsFile, err := writeHelmValuesToTempFile("", vals)
	if err != nil {
		return err
	}

	defer func() { _ = os.Remove(valsFile) }()

	return installHelmChart(attempt, destInfo, releaseName, vals)
}

func writePrivateKeyToTempFile(privateKey string) (string, error) {
	file, err := os.CreateTemp("", "pv_migrate_private_key")
	if err != nil {
		return "", fmt.Errorf("failed to create private key file: %w", err)
	}

	_, err = file.WriteString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to write private key to file: %w", err)
	}

	name := file.Name()

	err = os.Chmod(name, privateKeyFileMode) //nolint:gosec // name is from os.CreateTemp, not user input
	if err != nil {
		return "", fmt.Errorf("failed to chmod private key file: %w", err)
	}

	defer func() { _ = file.Close() }()

	return name, nil
}

func portForwardToSshd(ctx context.Context, pvcInfo *pvc.Info,
	helmReleaseName string, logger *slog.Logger,
) (int, chan<- struct{}, error) {
	sshdPod, err := getSshdPodForHelmRelease(ctx, pvcInfo, helmReleaseName)
	if err != nil {
		return 0, nil, err
	}

	restConfig := pvcInfo.ClusterClient.RestConfig
	namespace := sshdPod.Namespace
	name := sshdPod.Name

	port, err := getFreePort()
	if err != nil {
		return 0, nil, err
	}

	readyChan := make(chan struct{})
	stopChan := make(chan struct{})

	go func() {
		err := k8s.PortForward(&k8s.PortForwardRequest{
			RestConfig: restConfig,
			PodNs:      namespace,
			PodName:    name,
			LocalPort:  port,
			PodPort:    localSSHPort,
			StopCh:     stopChan,
			ReadyCh:    readyChan,
		}, logger)
		if err != nil {
			logger.Error(
				"❌ Error on port-forward",
				"ns",
				namespace,
				"name",
				name,
				"port",
				port,
				"error",
				err,
			)
		}
	}()

	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err() //nolint:wrapcheck
	case <-readyChan:
		return port, stopChan, nil

	//nolint:godox
	case <-time.After(portForwardTimeout): // todo: use context with timeout?
		return 0, nil, errors.New("timed out waiting for port-forward to be ready")
	}
}

// getFreePort asks the kernel for a free open port that is ready to usa.
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("error resolving tcp address: %w", err)
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("error listening to tcp address: %w", err)
	}

	defer func() { _ = listener.Close() }()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("error casting listener address to tcp address")
	}

	return tcpAddr.Port, nil
}
