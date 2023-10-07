package strategy

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"

	"github.com/utkuozdemir/pv-migrate/k8s"
	applog "github.com/utkuozdemir/pv-migrate/log"
	"github.com/utkuozdemir/pv-migrate/migration"
	"github.com/utkuozdemir/pv-migrate/pvc"
	"github.com/utkuozdemir/pv-migrate/rsync"
	"github.com/utkuozdemir/pv-migrate/ssh"
)

const (
	portForwardTimeout   = 30 * time.Second
	sshReverseTunnelPort = 50000

	localSSHPort       = 22
	privateKeyFileMode = 0o600
)

type Local struct{}

func (r *Local) Run(ctx context.Context, attempt *migration.Attempt) error {
	_, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("ssh binary not found")
	}

	mig := attempt.Migration
	sourceInfo := mig.SourceInfo
	destInfo := mig.DestInfo

	srcReleaseName, destReleaseName, privateKey, err := r.installLocalReleases(attempt)
	if err != nil {
		return fmt.Errorf("failed to install local releases: %w", err)
	}

	releaseNames := []string{srcReleaseName, destReleaseName}

	doneCh := registerCleanupHook(attempt, releaseNames)
	defer cleanupAndReleaseHook(attempt, releaseNames, doneCh)

	srcFwdPort, srcStopChan, err := portForwardToSshd(ctx, mig.Logger, sourceInfo, srcReleaseName)
	if err != nil {
		return fmt.Errorf("failed to port-forward to source: %w", err)
	}

	defer func() { srcStopChan <- struct{}{} }()

	destFwdPort, destStopChan, err := portForwardToSshd(ctx, mig.Logger, destInfo, destReleaseName)
	if err != nil {
		return fmt.Errorf("failed to port-forward to destination: %w", err)
	}

	defer func() { destStopChan <- struct{}{} }()

	privateKeyFile, err := writePrivateKeyToTempFile(privateKey)
	if err != nil {
		return fmt.Errorf("failed to write private key to temp file: %w", err)
	}

	defer func() { _ = os.Remove(privateKeyFile) }()

	rsyncCmd, err := buildRsyncCmdLocal(mig)
	if err != nil {
		return fmt.Errorf("failed to build rsync command: %w", err)
	}

	cmd := exec.Command("ssh", "-i", privateKeyFile,
		"-p", strconv.Itoa(srcFwdPort),
		"-R", fmt.Sprintf("%d:localhost:%d", sshReverseTunnelPort, destFwdPort),
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "root@localhost",
		rsyncCmd,
	)

	if err = runCmdLocal(attempt, cmd); err != nil {
		return fmt.Errorf("failed to run rsync command: %w", err)
	}

	return nil
}

func runCmdLocal(attempt *migration.Attempt, cmd *exec.Cmd) error {
	mig := attempt.Migration

	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer

	errorCh := make(chan error)

	go func() { errorCh <- cmd.Run() }()

	showProgressBar := !attempt.Migration.Request.NoProgressBar &&
		mig.Logger.Context.Value(applog.FormatContextKey) == applog.FormatFancy
	successCh := make(chan bool, 1)

	logTail := rsync.LogTail{
		LogReaderFunc:   func() (io.ReadCloser, error) { return reader, nil },
		SuccessCh:       successCh,
		ShowProgressBar: showProgressBar,
		Logger:          mig.Logger,
	}

	go logTail.Start()

	err := <-errorCh
	successCh <- err == nil

	return err
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

	cmd, err := rsyncCmd.Build()
	if err != nil {
		return "", fmt.Errorf("failed to build rsync command: %w", err)
	}

	return cmd, nil
}

func (r *Local) installLocalReleases(attempt *migration.Attempt) (string, string, string, error) {
	attempt.Migration.Logger.Info("🔑 Generating SSH key pair")
	keyAlgorithm := attempt.Migration.Request.KeyAlgorithm

	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate SSH key pair: %w", err)
	}

	privateKeyMountPath := "/tmp/id_" + keyAlgorithm

	srcReleaseName := attempt.HelmReleaseNamePrefix + "-src"
	destReleaseName := attempt.HelmReleaseNamePrefix + "-dest"

	err = installLocalOnSource(attempt, srcReleaseName, publicKey,
		privateKey, privateKeyMountPath, srcMountPath)
	if err != nil {
		return "", "", "", err
	}

	err = installLocalOnDest(attempt, destReleaseName, publicKey, destMountPath)
	if err != nil {
		return "", "", "", err
	}

	return srcReleaseName, destReleaseName, privateKey, nil
}

func getSshdPodForHelmRelease(ctx context.Context, pvcInfo *pvc.Info, name string) (*corev1.Pod, error) {
	labelSelector := "app.kubernetes.io/component=sshd,app.kubernetes.io/instance=" + name

	pod, err := k8s.WaitForPod(ctx, pvcInfo.ClusterClient.KubeClient, pvcInfo.Claim.Namespace, labelSelector)
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

	err = os.Chmod(name, privateKeyFileMode)
	if err != nil {
		return "", fmt.Errorf("failed to chmod private key file: %w", err)
	}

	defer func() { _ = file.Close() }()

	return name, nil
}

func portForwardToSshd(ctx context.Context, logger *log.Entry,
	pvcInfo *pvc.Info, helmReleaseName string,
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
				WithField("port", port).Error("❌ Error on port-forward")
		}
	}()

	select {
	case <-readyChan:
		return port, stopChan, nil
	case <-time.After(portForwardTimeout):
		return 0, nil, fmt.Errorf("timed out waiting for port-forward to be ready")
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
		return 0, fmt.Errorf("error casting listener address to tcp address")
	}

	return tcpAddr.Port, nil
}
