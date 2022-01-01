package strategy

import (
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	applog "github.com/utkuozdemir/pv-migrate/internal/log"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/migration"
	"io"
	"io/ioutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const (
	portForwardTimeout   = 30 * time.Second
	sshReverseTunnelPort = 50000
)

type Local struct {
}

func (r *Local) Run(a *migration.Attempt) (bool, error) {
	_, err := exec.LookPath("ssh")
	if err != nil {
		return false, fmt.Errorf(":cross_mark: Error: binary not found in path: %s", "ssh")
	}

	t := a.Migration
	s := t.SourceInfo
	d := t.DestInfo

	t.Logger.Info(":key: Generating SSH key pair")
	keyAlgorithm := t.Request.KeyAlgorithm
	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return true, err
	}
	privateKeyMountPath := "/root/.ssh/id_" + keyAlgorithm

	srcReleaseName := a.HelmReleaseNamePrefix + "-src"
	destReleaseName := a.HelmReleaseNamePrefix + "-dest"
	releaseNames := []string{srcReleaseName, destReleaseName}

	doneCh := registerCleanupHook(a, releaseNames)
	defer cleanupAndReleaseHook(a, releaseNames, doneCh)

	srcMountPath := "/source"
	destMountPath := "/dest"

	err = installLocalOnSource(a, srcReleaseName, publicKey,
		privateKey, privateKeyMountPath, srcMountPath)
	if err != nil {
		return true, err
	}

	err = installLocalOnDest(a, destReleaseName, publicKey, destMountPath)
	if err != nil {
		return true, err
	}

	sourceSshdPod, err := getSshdPodForHelmRelease(s, srcReleaseName)
	if err != nil {
		return true, err
	}

	srcFwdPort, srcStopChan, err := portForwardForPod(t.Logger, s.ClusterClient.RestConfig,
		sourceSshdPod.Namespace, sourceSshdPod.Name)
	if err != nil {
		return true, err
	}
	defer func() { srcStopChan <- struct{}{} }()

	destSshdPod, err := getSshdPodForHelmRelease(d, destReleaseName)
	if err != nil {
		return true, err
	}

	destFwdPort, destStopChan, err := portForwardForPod(t.Logger, d.ClusterClient.RestConfig,
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

	srcPath := srcMountPath + "/" + t.Request.Source.Path
	destPath := destMountPath + "/" + t.Request.Dest.Path

	rsyncCmd := rsync.Cmd{
		Port:        sshReverseTunnelPort,
		NoChown:     t.Request.NoChown,
		Delete:      t.Request.DeleteExtraneousFiles,
		SrcPath:     srcPath,
		DestPath:    destPath,
		DestUseSsh:  true,
		DestSshHost: "localhost",
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

	showProgressBar := !a.Migration.Request.NoProgressBar &&
		t.Logger.Context.Value(applog.FormatContextKey) == applog.FormatFancy
	successCh := make(chan bool, 1)

	logTail := rsync.LogTail{
		LogReaderFunc:   func() (io.ReadCloser, error) { return reader, nil },
		SuccessCh:       successCh,
		ShowProgressBar: showProgressBar,
		Logger:          t.Logger,
	}

	go logTail.Start()

	err = <-errorCh
	successCh <- err == nil
	return true, err
}

func getSshdPodForHelmRelease(pvcInfo *pvc.Info, name string) (*corev1.Pod, error) {
	labelSelector := "app.kubernetes.io/component=sshd,app.kubernetes.io/instance=" + name
	return k8s.WaitForPod(pvcInfo.ClusterClient.KubeClient, pvcInfo.Claim.Namespace, labelSelector)
}

func installLocalOnSource(a *migration.Attempt, releaseName,
	publicKey, privateKey, privateKeyMountPath, srcMountPath string) error {
	t := a.Migration
	s := t.SourceInfo
	ns := s.Claim.Namespace

	vals := map[string]interface{}{
		"sshd": map[string]interface{}{
			"enabled":             true,
			"namespace":           ns,
			"publicKey":           publicKey,
			"privateKeyMount":     true,
			"privateKey":          privateKey,
			"privateKeyMountPath": privateKeyMountPath,
			"pvcMounts": []map[string]interface{}{
				{
					"name":      s.Claim.Name,
					"readOnly":  t.Request.SourceMountReadOnly,
					"mountPath": srcMountPath,
				},
			},
		},
	}

	return installHelmChart(a, s, releaseName, vals)
}

func installLocalOnDest(a *migration.Attempt, releaseName, publicKey, destMountPath string) error {
	t := a.Migration
	d := t.DestInfo
	ns := d.Claim.Namespace

	vals := map[string]interface{}{
		"sshd": map[string]interface{}{
			"enabled":   true,
			"namespace": ns,
			"publicKey": publicKey,
			"pvcMounts": []map[string]interface{}{
				{
					"name":      d.Claim.Name,
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

	return installHelmChart(a, d, releaseName, vals)
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

	err = os.Chmod(name, 0600)
	if err != nil {
		return "", err
	}

	defer func() { _ = file.Close() }()
	return name, nil
}

func portForwardForPod(logger *log.Entry, restConfig *rest.Config,
	ns, name string) (int, chan<- struct{}, error) {
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
			PodNs:      ns,
			PodName:    name,
			LocalPort:  port,
			PodPort:    22,
			StopCh:     stopChan,
			ReadyCh:    readyChan,
		})
		if err != nil {
			logger.WithError(err).WithField("ns", ns).WithField("name", name).
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

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}
