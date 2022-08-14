package strategy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	applog "github.com/utkuozdemir/pv-migrate/internal/log"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/migration"
)

const (
	portForwardTimeout   = 30 * time.Second
	sshReverseTunnelPort = 50000

	localSSHPort       = 22
	privateKeyFileMode = 0o600
)

var (
	ErrNoTCPAddress       = errors.New("could not get TCP address from listener")
	ErrPortForwardTimeout = errors.New("timed out waiting for port-forward to be ready")
	ErrSSHBinaryNotFound  = errors.New("ssh binary not found")
)

type Local struct{}

//nolint:cyclop,funlen
func (r *Local) Run(ctx context.Context, attempt *migration.Attempt) (bool, error) {
	_, err := exec.LookPath("ssh")
	if err != nil {
		return false, ErrSSHBinaryNotFound
	}

	mig := attempt.Migration
	sourceInfo := mig.SourceInfo
	destInfo := mig.DestInfo

	srcReleaseName, destReleaseName, privateKey, err := r.installLocalReleases(ctx, attempt)
	if err != nil {
		return true, err
	}

	releaseNames := []string{srcReleaseName, destReleaseName}

	doneCh := registerCleanupHook(ctx, attempt, releaseNames)
	defer cleanupAndReleaseHook(ctx, attempt, releaseNames, doneCh)

	srcFwdPort, srcStopChan, err := portForwardToSshd(ctx, mig.Logger, sourceInfo, srcReleaseName)
	if err != nil {
		return true, err
	}

	defer func() {
		srcStopChan <- struct{}{}

		select {
		case srcStopChan <- struct{}{}:
		case <-ctx.Done():
		}
	}()

	destFwdPort, destStopChan, err := portForwardToSshd(ctx, mig.Logger, destInfo, destReleaseName)
	if err != nil {
		return true, err
	}

	defer func() {
		destStopChan <- struct{}{}

		select {
		case destStopChan <- struct{}{}:
		case <-ctx.Done():
		}
	}()

	privateKeyFile, err := writePrivateKeyToTempFile(privateKey)
	defer func() { _ = os.Remove(privateKeyFile) }()

	if err != nil {
		return true, err
	}

	rsyncCmd, err := buildRsyncCmdLocal(mig)
	if err != nil {
		return true, err
	}

	cmd := exec.CommandContext(ctx, "ssh", "-i", privateKeyFile,
		"-p", strconv.Itoa(srcFwdPort),
		"-R", fmt.Sprintf("%d:localhost:%d", sshReverseTunnelPort, destFwdPort),
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "root@localhost",
		rsyncCmd,
	)

	err = runCmdLocal(ctx, attempt, cmd)

	return true, err
}

func runCmdLocal(ctx context.Context, attempt *migration.Attempt, cmd *exec.Cmd) error {
	mig := attempt.Migration

	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer

	var errGroup errgroup.Group

	errGroup.Go(cmd.Run)

	showProgressBar := !attempt.Migration.Request.NoProgressBar &&
		mig.Logger.Context.Value(applog.FormatContextKey) == applog.FormatFancy
	successCh := make(chan bool, 1)

	logTail := rsync.LogTail{
		LogReaderFunc:   func() (io.ReadCloser, error) { return reader, nil },
		SuccessCh:       successCh,
		ShowProgressBar: showProgressBar,
		Logger:          mig.Logger,
	}

	go logTail.Start(ctx)

	err := errGroup.Wait()

	select {
	case successCh <- err == nil:
		return err
	case <-ctx.Done():
		return ctx.Err()
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
	}

	return rsyncCmd.Build()
}

func (r *Local) installLocalReleases(ctx context.Context, attempt *migration.Attempt) (string, string, string, error) {
	attempt.Migration.Logger.Info(":key: Generating SSH key pair")
	keyAlgorithm := attempt.Migration.Request.KeyAlgorithm

	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return "", "", "", err
	}

	privateKeyMountPath := "/root/.ssh/id_" + keyAlgorithm

	srcReleaseName := attempt.HelmReleaseNamePrefix + "-src"
	destReleaseName := attempt.HelmReleaseNamePrefix + "-dest"

	err = installLocalOnSource(ctx, attempt, srcReleaseName, publicKey,
		privateKey, privateKeyMountPath, srcMountPath)
	if err != nil {
		return "", "", "", err
	}

	err = installLocalOnDest(ctx, attempt, destReleaseName, publicKey, destMountPath)
	if err != nil {
		return "", "", "", err
	}

	return srcReleaseName, destReleaseName, privateKey, nil
}

func getSshdPodForHelmRelease(ctx context.Context, pvcInfo *pvc.Info, name string) (*corev1.Pod, error) {
	labelSelector := "app.kubernetes.io/component=sshd,app.kubernetes.io/instance=" + name

	return k8s.WaitForPod(ctx, pvcInfo.ClusterClient.KubeClient, pvcInfo.Claim.Namespace, labelSelector)
}

func installLocalOnSource(ctx context.Context, attempt *migration.Attempt, releaseName,
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

	return installHelmChart(ctx, attempt, sourceInfo, releaseName, vals)
}

func installLocalOnDest(
	ctx context.Context,
	attempt *migration.Attempt,
	releaseName, publicKey, destMountPath string,
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

	valsFile, err := writeHelmValuesToTempFile("", vals)
	if err != nil {
		return err
	}

	defer func() { _ = os.Remove(valsFile) }()

	return installHelmChart(ctx, attempt, destInfo, releaseName, vals)
}

func writePrivateKeyToTempFile(privateKey string) (string, error) {
	file, err := os.CreateTemp("", "pv_migrate_private_key")
	if err != nil {
		return "", err
	}

	_, err = file.WriteString(privateKey)
	if err != nil {
		return "", err
	}

	name := file.Name()

	err = os.Chmod(name, privateKeyFileMode)
	if err != nil {
		return "", err
	}

	defer func() { _ = file.Close() }()

	return name, nil
}

func portForwardToSshd(
	ctx context.Context,
	logger *log.Entry,
	pvcInfo *pvc.Info,
	helmReleaseName string,
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
			Logger:     logger,
			PodNs:      namespace,
			PodName:    name,
			LocalPort:  port,
			PodPort:    localSSHPort,
			StopCh:     stopChan,
			ReadyCh:    readyChan,
		})
		if err != nil {
			logger.WithError(err).WithField("ns", namespace).WithField("name", name).
				WithField("port", port).Error(":cross_mark: Error on port-forward")
		}
	}()

	select {
	case <-readyChan:
		return port, stopChan, nil
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-time.After(portForwardTimeout):
		return 0, nil, ErrPortForwardTimeout
	}
}

// getFreePort asks the kernel for a free open port that is ready to usa.
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}

	defer func() { _ = listener.Close() }()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, ErrNoTCPAddress
	}

	return tcpAddr.Port, nil
}
