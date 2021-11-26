package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"strconv"
)

type Svc struct {
}

func (r *Svc) canDo(t *task.Task) bool {
	s := t.SourceInfo
	d := t.DestInfo
	sameCluster := s.ClusterClient == d.ClusterClient
	return sameCluster
}

func (r *Svc) Run(e *task.Execution) (bool, error) {
	t := e.Task
	if !r.canDo(t) {
		return false, nil
	}

	s := e.Task.SourceInfo
	d := e.Task.DestInfo
	sourceNs := s.Claim.Namespace
	destNs := d.Claim.Namespace
	opts := t.Migration.Options

	t.Logger.Info(":key: Generating SSH key pair")
	keyAlgorithm := t.Migration.Options.KeyAlgorithm
	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return true, err
	}
	privateKeyMountPath := "/root/.ssh/id_" + keyAlgorithm

	releaseName := e.HelmReleaseNamePrefix
	releaseNames := []string{releaseName}

	sshRemoteHost := releaseName + "-sshd." + sourceNs

	srcMountPath := "/source"
	destMountPath := "/dest"
	helmValues := []string{
		"rsync.enabled=true",
		"rsync.namespace=" + destNs,
		"rsync.deleteExtraneousFiles=" + strconv.FormatBool(opts.DeleteExtraneousFiles),
		"rsync.noChown=" + strconv.FormatBool(opts.NoChown),
		"rsync.privateKeyMount=true",
		"rsync.privateKey=" + privateKey,
		"rsync.privateKeyMountPath=" + privateKeyMountPath,
		"rsync.pvcMounts[0].name=" + d.Claim.Name,
		"rsync.pvcMounts[0].mountPath=" + destMountPath,
		"rsync.sourcePath=" + srcMountPath + "/" + t.Migration.Source.Path,
		"rsync.destPath=" + destMountPath + "/" + t.Migration.Dest.Path,
		"rsync.useSsh=true",
		"rsync.sshRemoteHost=" + sshRemoteHost,
		"sshd.enabled=true",
		"sshd.namespace=" + sourceNs,
		"sshd.publicKey=" + publicKey,
		"sshd.pvcMounts[0].name=" + s.Claim.Name,
		"sshd.pvcMounts[0].mountPath=" + srcMountPath,
		"sshd.pvcMounts[0].readOnly=" + strconv.FormatBool(opts.SourceMountReadOnly),
	}

	doneCh := registerCleanupHook(e, releaseNames)
	defer cleanupAndReleaseHook(e, releaseNames, doneCh)

	err = installHelmChart(e, d, releaseName, helmValues)
	if err != nil {
		return true, err
	}

	showProgressBar := !opts.NoProgressBar
	kubeClient := t.SourceInfo.ClusterClient.KubeClient
	jobName := releaseName + "-rsync"
	err = k8s.WaitForJobCompletion(e.Logger, kubeClient, destNs, jobName, showProgressBar)
	return true, err
}
