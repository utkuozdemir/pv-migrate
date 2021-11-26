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

	helmValues := []string{
		"rsync.enabled=true",
		"rsync.deleteExtraneousFiles=" + strconv.FormatBool(opts.DeleteExtraneousFiles),
		"rsync.noChown=" + strconv.FormatBool(opts.NoChown),
		"rsync.privateKeyMount=true",
		"rsync.privateKey=" + privateKey,
		"rsync.privateKeyMountPath=" + privateKeyMountPath,
		"sshd.enabled=true",
		"sshd.publicKey=" + publicKey,
		"source.namespace=" + sourceNs,
		"source.pvcName=" + s.Claim.Name,
		"source.pvcMountReadOnly=" + strconv.FormatBool(opts.SourceMountReadOnly),
		"source.path=" + t.Migration.Source.Path,
		"dest.namespace=" + destNs,
		"dest.pvcName=" + d.Claim.Name,
		"dest.path=" + t.Migration.Dest.Path,
	}

	releaseName := e.HelmReleaseNamePrefix
	releaseNames := []string{releaseName}

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
