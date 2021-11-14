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

	doneCh := registerCleanupHook(e)
	defer cleanupAndReleaseHook(e, doneCh)

	err = installOnSource(e, publicKey)
	if err != nil {
		return true, err
	}

	sourceKubeClient := e.Task.SourceInfo.ClusterClient.KubeClient
	svcName := fmt.Sprintf("pv-migrate-%s-sshd", e.ID)
	lbSvcAddress, err := k8s.GetServiceAddress(t.Logger, sourceKubeClient, sourceNs, svcName)
	if err != nil {
		return true, err
	}

	sshTargetHost := formatSSHTargetHost(lbSvcAddress)

	err = installOnDest(e, privateKey, privateKeyMountPath, sshTargetHost)
	if err != nil {
		return true, err
	}

	showProgressBar := !e.Task.Migration.Options.NoProgressBar
	kubeClient := s.ClusterClient.KubeClient
	jobName := e.HelmReleaseName + "-rsync"
	err = k8s.WaitUntilJobIsCompleted(e.Logger, kubeClient, destNs, jobName, showProgressBar)
	return true, err
}

func installOnSource(e *task.Execution, publicKey string) error {
	t := e.Task
	s := t.SourceInfo
	ns := s.Claim.Namespace
	opts := t.Migration.Options

	helmValues := []string{
		"sshd.enabled=true",
		"sshd.publicKey=" + publicKey,
		"sshd.service.type=LoadBalancer",
		"source.namespace=" + ns,
		"source.pvcName=" + s.Claim.Name,
		"source.pvcMountReadOnly=" + strconv.FormatBool(opts.SourceMountReadOnly),
		"source.path=" + t.Migration.Source.Path,
	}

	return installHelmChart(e, s, helmValues)
}

func installOnDest(e *task.Execution, privateKey string, privateKeyMountPath string, sshHost string) error {
	t := e.Task
	d := t.DestInfo
	ns := d.Claim.Namespace
	opts := t.Migration.Options
	helmValues := []string{
		"rsync.enabled=true",
		"rsync.deleteExtraneousFiles=" + strconv.FormatBool(opts.DeleteExtraneousFiles),
		"rsync.noChown=" + strconv.FormatBool(opts.NoChown),
		"rsync.privateKeyMount=true",
		"rsync.privateKey=" + privateKey,
		"rsync.privateKeyMountPath=" + privateKeyMountPath,
		"rsync.sshRemoteHost=" + sshHost,
		"source.path=" + t.Migration.Source.Path,
		"dest.namespace=" + ns,
		"dest.pvcName=" + d.Claim.Name,
		"dest.path=" + t.Migration.Dest.Path,
	}

	return installHelmChart(e, d, helmValues)
}

func formatSSHTargetHost(host string) string {
	if util.IsIPv6(host) {
		return fmt.Sprintf("[%s]", host)
	}
	return host
}
