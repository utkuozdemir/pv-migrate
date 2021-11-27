package strategy

import (
	"fmt"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"github.com/utkuozdemir/pv-migrate/internal/util"
	"strconv"
)

type LbSvc struct {
}

func (r *LbSvc) Run(e *task.Execution) (bool, error) {
	t := e.Task

	s := t.SourceInfo
	d := t.DestInfo
	sourceNs := s.Claim.Namespace
	destNs := d.Claim.Namespace

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

	err = installOnSource(e, srcReleaseName, publicKey, srcMountPath)
	if err != nil {
		return true, err
	}

	sourceKubeClient := e.Task.SourceInfo.ClusterClient.KubeClient
	svcName := srcReleaseName + "-sshd"
	lbSvcAddress, err := k8s.GetServiceAddress(sourceKubeClient, sourceNs, svcName)
	if err != nil {
		return true, err
	}

	sshTargetHost := formatSSHTargetHost(lbSvcAddress)

	err = installOnDest(e, destReleaseName, privateKey, privateKeyMountPath,
		sshTargetHost, srcMountPath, destMountPath)
	if err != nil {
		return true, err
	}

	showProgressBar := !e.Task.Migration.Options.NoProgressBar
	kubeClient := s.ClusterClient.KubeClient
	jobName := destReleaseName + "-rsync"
	err = k8s.WaitForJobCompletion(e.Logger, kubeClient, destNs, jobName, showProgressBar)
	return true, err
}

func installOnSource(e *task.Execution, releaseName, publicKey, srcMountPath string) error {
	t := e.Task
	s := t.SourceInfo
	ns := s.Claim.Namespace
	opts := t.Migration.Options

	helmValues := []string{
		"sshd.enabled=true",
		"sshd.namespace=" + ns,
		"sshd.publicKey=" + publicKey,
		"sshd.service.type=LoadBalancer",
		"sshd.pvcMounts[0].name=" + s.Claim.Name,
		"sshd.pvcMounts[0].readOnly=" + strconv.FormatBool(opts.SourceMountReadOnly),
		"sshd.pvcMounts[0].mountPath=" + srcMountPath,
	}

	return installHelmChart(e, s, releaseName, helmValues)
}

func installOnDest(e *task.Execution, releaseName, privateKey,
	privateKeyMountPath, sshHost, srcMountPath, destMountPath string) error {
	t := e.Task
	d := t.DestInfo
	ns := d.Claim.Namespace
	opts := t.Migration.Options

	helmValues := []string{
		"rsync.enabled=true",
		"rsync.namespace=" + ns,
		"rsync.deleteExtraneousFiles=" + strconv.FormatBool(opts.DeleteExtraneousFiles),
		"rsync.noChown=" + strconv.FormatBool(opts.NoChown),
		"rsync.privateKeyMount=true",
		"rsync.privateKey=" + privateKey,
		"rsync.privateKeyMountPath=" + privateKeyMountPath,
		"rsync.sshRemoteHost=" + sshHost,
		"rsync.pvcMounts[0].name=" + d.Claim.Name,
		"rsync.pvcMounts[0].mountPath=" + destMountPath,
		"rsync.sourcePath=" + srcMountPath + "/" + t.Migration.Source.Path,
		"rsync.destPath=" + destMountPath + "/" + t.Migration.Dest.Path,
		"rsync.useSsh=true",
	}

	return installHelmChart(e, d, releaseName, helmValues)
}

func formatSSHTargetHost(host string) string {
	if util.IsIPv6(host) {
		return fmt.Sprintf("[%s]", host)
	}
	return host
}
