package strategy

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	applog "github.com/utkuozdemir/pv-migrate/internal/log"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/migration"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

const (
	portForwardTimeout   = 30 * time.Second
	sshReverseTunnelPort = 50000

	localSrcMountPath  = "/source"
	localDestMountPath = "/dest"

	localSSHPort       = 22
	privateKeyFileMode = 0o600
)

type Local struct{}

func (r *Local) Run(attempt *migration.Attempt) (bool, error) {
	_, err := exec.LookPath("ssh")
	if err != nil {
		return false, fmt.Errorf(":cross_mark: Error: binary not found in path: %s", "ssh")
	}

	migration := attempt.Migration
	sourceInfo := migration.SourceInfo
	destInfo := migration.DestInfo

	srcReleaseName, destReleaseName, privateKey, err := r.installLocalReleases(attempt)
	if err != nil {
		return true, err
	}

	releaseNames := []string{srcReleaseName, destReleaseName}

	doneCh := registerCleanupHook(attempt, releaseNames)
	defer cleanupAndReleaseHook(attempt, releaseNames, doneCh)

	sourceSshdPod, err := getSshdPodForHelmRelease(sourceInfo, srcReleaseName)
	if err != nil {
		return true, err
	}

	srcFwdPort, srcStopChan, err := portForwardForPod(migration.Logger, sourceInfo.ClusterClient.RestConfig,
		sourceSshdPod.Namespace, sourceSshdPod.Name)
	if err != nil {
		return true, err
	}
	defer func() { srcStopChan <- struct{}{} }()

	destSshdPod, err := getSshdPodForHelmRelease(destInfo, destReleaseName)
	if err != nil {
		return true, err
	}

	destFwdPort, destStopChan, err := portForwardForPod(migration.Logger, destInfo.ClusterClient.RestConfig,
		destSshdPod.Namespace, destSshdPod.Name)
	if err != nil {
		return true, err
	}

	defer func() { destStopChan <- struct{}{} }()

	privateKeyFile, err := writePrivateKeyToTempFile(privateKey)
	defer func() { _ = os.Remove(privateKeyFile) }()

	if err != nil {
		return true, err
	}

	srcPath := localSrcMountPath + "/" + migration.Request.Source.Path
	destPath := localDestMountPath + "/" + migration.Request.Dest.Path

	rsyncCmd := rsync.Cmd{
		Port:        sshReverseTunnelPort,
		NoChown:     migration.Request.NoChown,
		Delete:      migration.Request.DeleteExtraneousFiles,
		SrcPath:     srcPath,
		DestPath:    destPath,
		DestUseSSH:  true,
		DestSSHHost: "localhost",
	}
	rsyncCmdStr, err := rsyncCmd.Build()
	if err != nil {
		return true, err
	}

	cmd := exec.Command("ssh", "-i", privateKeyFile,
		"-p", strconv.Itoa(srcFwdPort),
		"-R", fmt.Sprintf("%d:localhost:%d", sshReverseTunnelPort, destFwdPort),
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "root@localhost",
		rsyncCmdStr,
	)

	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer

	errorCh := make(chan error)
	go func() { errorCh <- cmd.Run() }()

	showProgressBar := !attempt.Migration.Request.NoProgressBar &&
		migration.Logger.Context.Value(applog.FormatContextKey) == applog.FormatFancy
	successCh := make(chan bool, 1)

	logTail := rsync.LogTail{
		LogReaderFunc:   func() (io.ReadCloser, error) { return reader, nil },
		SuccessCh:       successCh,
		ShowProgressBar: showProgressBar,
		Logger:          migration.Logger,
	}

	go logTail.Start()

	err = <-errorCh
	successCh <- err == nil

	return true, err
}

func (r *Local) installLocalReleases(attempt *migration.Attempt) (srcReleaseName, destReleaseName,
	privateKey string, err error,
) {
	attempt.Migration.Logger.Info(":key: Generating SSH key pair")
	keyAlgorithm := attempt.Migration.Request.KeyAlgorithm
	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return
	}

	privateKeyMountPath := "/root/.ssh/id_" + keyAlgorithm

	srcReleaseName = attempt.HelmReleaseNamePrefix + "-src"
	destReleaseName = attempt.HelmReleaseNamePrefix + "-dest"

	err = installLocalOnSource(attempt, srcReleaseName, publicKey,
		privateKey, privateKeyMountPath, localSrcMountPath)
	if err != nil {
		return
	}

	err = installLocalOnDest(attempt, destReleaseName, publicKey, localDestMountPath)

	return
}

func getSshdPodForHelmRelease(pvcInfo *pvc.Info, name string) (*corev1.Pod, error) {
	labelSelector := "app.kubernetes.io/component=sshd,app.kubernetes.io/instance=" + name

	return k8s.WaitForPod(pvcInfo.ClusterClient.KubeClient, pvcInfo.Claim.Namespace, labelSelector)
}

func installLocalOnSource(attempt *migration.Attempt, releaseName,
	publicKey, privateKey, privateKeyMountPath, srcMountPath string,
) error {
	mig := attempt.Migration
	sourceInfo := mig.SourceInfo
	namespace := sourceInfo.Claim.Namespace

	vals := map[string]interface{}{
		"sshd": map[string]interface{}{
			"enabled":             true,
			"namespace":           namespace,
			"publicKey":           publicKey,
			"privateKeyMount":     true,
			"privateKey":          privateKey,
			"privateKeyMountPath": privateKeyMountPath,
			"pvcMounts": []map[string]interface{}{
				{
					"name":      sourceInfo.Claim.Name,
					"readOnly":  mig.Request.SourceMountReadOnly,
					"mountPath": srcMountPath,
				},
			},
		},
	}

	return installHelmChart(attempt, sourceInfo, releaseName, vals)
}

func installLocalOnDest(attempt *migration.Attempt, releaseName, publicKey, destMountPath string) error {
	migration := attempt.Migration
	destInfo := migration.DestInfo
	namespace := destInfo.Claim.Namespace

	vals := map[string]interface{}{
		"sshd": map[string]interface{}{
			"enabled":   true,
			"namespace": namespace,
			"publicKey": publicKey,
			"pvcMounts": []map[string]interface{}{
				{
					"name":      destInfo.Claim.Name,
					"mountPath": destMountPath,
				},
			},
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
	file, err := ioutil.TempFile("", "pv_migrate_private_key")
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

func portForwardForPod(logger *log.Entry, restConfig *rest.Config,
	namespace, name string,
) (int, chan<- struct{}, error) {
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
	case <-time.After(portForwardTimeout):
		return 0, nil, errors.New("timed out waiting for port-forward to be ready")
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
		return 0, errors.New("could not get TCP address from listener")
	}

	return tcpAddr.Port, nil
}
