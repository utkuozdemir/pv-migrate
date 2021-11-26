package strategy

import (
	"fmt"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"io/ioutil"
	corev1 "k8s.io/api/core/v1"
	"os"
	"os/exec"
	"strconv"
)

type Local struct {
}

func (r *Local) Run(e *task.Execution) (bool, error) {
	_, err := exec.LookPath("ssh")
	if err != nil {
		return false, fmt.Errorf(":cross_mark: Error: binary not found in path: %s", "ssh")
	}

	t := e.Task
	s := t.SourceInfo
	d := t.DestInfo

	t.Logger.Info(":key: Generating SSH key pair")
	keyAlgorithm := t.Migration.Options.KeyAlgorithm
	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return true, err
	}
	privateKeyMountPath := "/root/.ssh/id_" + keyAlgorithm

	srcReleaseName := e.HelmReleaseNamePrefix + "-src"
	destReleaseName := e.HelmReleaseNamePrefix + "-dest"
	releaseNames := []string{srcReleaseName, destReleaseName}

	doneCh := registerCleanupHook(e, releaseNames)
	defer cleanupAndReleaseHook(e, releaseNames, doneCh)

	srcMountPath := "/source"
	destMountPath := "/dest"

	err = installLocalOnSource(e, srcReleaseName, publicKey,
		privateKey, privateKeyMountPath, srcMountPath)
	if err != nil {
		return true, err
	}

	err = installLocalOnDest(e, destReleaseName, publicKey, destMountPath)
	if err != nil {
		return true, err
	}

	sourceSshdPod, err := getSshdPodForHelmRelease(s, srcReleaseName)
	if err != nil {
		return true, err
	}

	srcReadyChan := make(chan struct{})
	srcStopChan := make(chan struct{})
	defer close(srcStopChan)

	// todo: use custom ports

	go func() {
		err := k8s.PortForward(&k8s.PortForwardRequest{
			RestConfig: s.ClusterClient.RestConfig,
			Logger:     t.Logger,
			PodNs:      sourceSshdPod.Namespace,
			PodName:    sourceSshdPod.Name,
			LocalPort:  50022,
			PodPort:    22,
			StopCh:     srcStopChan,
			ReadyCh:    srcReadyChan,
		})
		if err != nil {
			// todo: handle error
		}
	}()

	destSshdPod, err := getSshdPodForHelmRelease(d, destReleaseName)
	if err != nil {
		return true, err
	}

	destReadyChan := make(chan struct{})
	destStopChan := make(chan struct{})
	defer close(destStopChan)

	go func() {
		err := k8s.PortForward(&k8s.PortForwardRequest{
			RestConfig: d.ClusterClient.RestConfig,
			Logger:     t.Logger,
			PodNs:      destSshdPod.Namespace,
			PodName:    destSshdPod.Name,
			LocalPort:  60022,
			PodPort:    22,
			StopCh:     destStopChan,
			ReadyCh:    destReadyChan,
		})
		if err != nil {
			// todo: handle error
		}
	}()

	privateKeyFile, err := writePrivateKeyToTempFile(privateKey)
	defer func() { _ = os.Remove(privateKeyFile) }()

	if err != nil {
		return true, err
	}

	<-srcReadyChan
	<-destReadyChan

	srcPath := srcMountPath + "/" + t.Migration.Source.Path
	destPath := destMountPath + "/" + t.Migration.Dest.Path

	cmd := exec.Command("ssh", "-i", privateKeyFile,
		"-p", "50022", "-R", "50001:localhost:60022",
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "root@localhost",
		"rsync -e 'ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p 50001' -vuar "+srcPath+" root@localhost:"+destPath,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// todo: progress bar

	err = cmd.Run()
	return true, err
}

func getSshdPodForHelmRelease(pvcInfo *pvc.Info, name string) (*corev1.Pod, error) {
	labelSelector := "app.kubernetes.io/component=sshd,app.kubernetes.io/instance=" + name
	return k8s.WaitForPod(pvcInfo.ClusterClient.KubeClient, pvcInfo.Claim.Namespace, labelSelector)
}

func installLocalOnSource(e *task.Execution, releaseName,
	publicKey, privateKey, privateKeyMountPath, srcMountPath string) error {
	t := e.Task
	s := t.SourceInfo
	ns := s.Claim.Namespace
	opts := t.Migration.Options

	helmValues := []string{
		"sshd.enabled=true",
		"sshd.namespace=" + ns,
		"sshd.publicKey=" + publicKey,
		"sshd.privateKeyMount=true",
		"sshd.privateKey=" + privateKey,
		"sshd.privateKeyMountPath=" + privateKeyMountPath,
		"sshd.pvcMounts[0].name=" + s.Claim.Name,
		"sshd.pvcMounts[0].readOnly=" + strconv.FormatBool(opts.SourceMountReadOnly),
		"sshd.pvcMounts[0].mountPath=" + srcMountPath,
	}

	return installHelmChart(e, s, releaseName, helmValues)
}

func installLocalOnDest(e *task.Execution, releaseName, publicKey, destMountPath string) error {
	t := e.Task
	d := t.DestInfo
	ns := d.Claim.Namespace
	helmValues := []string{
		"sshd.enabled=true",
		"sshd.namespace=" + ns,
		"sshd.publicKey=" + publicKey,
		"sshd.pvcMounts[0].name=" + d.Claim.Name,
		"sshd.pvcMounts[0].mountPath=" + destMountPath,
	}

	return installHelmChart(e, d, releaseName, helmValues)
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
