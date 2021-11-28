package strategy

import (
	"fmt"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"github.com/utkuozdemir/pv-migrate/internal/util"
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

	vals := map[string]interface{}{
		"sshd": map[string]interface{}{
			"enabled":   true,
			"namespace": ns,
			"publicKey": publicKey,
			"service": map[string]interface{}{
				"type": "LoadBalancer",
			},
			"pvcMounts": []map[string]interface{}{
				{
					"name":      s.Claim.Name,
					"readOnly":  opts.SourceMountReadOnly,
					"mountPath": srcMountPath,
				},
			},
		},
	}

	return installHelmChart(e, s, releaseName, vals)
}

func installOnDest(e *task.Execution, releaseName, privateKey,
	privateKeyMountPath, sshHost, srcMountPath, destMountPath string) error {
	t := e.Task
	d := t.DestInfo
	ns := d.Claim.Namespace
	opts := t.Migration.Options

	srcPath := srcMountPath + "/" + t.Migration.Source.Path
	destPath := destMountPath + "/" + t.Migration.Dest.Path
	rsyncCmd := rsync.Cmd{
		NoChown:    opts.NoChown,
		Delete:     opts.DeleteExtraneousFiles,
		SrcPath:    srcPath,
		DestPath:   destPath,
		SrcUseSsh:  true,
		SrcSshHost: sshHost,
	}
	rsyncCmdStr, err := rsyncCmd.Build()
	if err != nil {
		return err
	}

	vals := map[string]interface{}{
		"rsync": map[string]interface{}{
			"enabled":             true,
			"namespace":           ns,
			"privateKeyMount":     true,
			"privateKey":          privateKey,
			"privateKeyMountPath": privateKeyMountPath,
			"sshRemoteHost":       sshHost,
			"pvcMounts": []map[string]interface{}{
				{
					"name":      d.Claim.Name,
					"mountPath": destMountPath,
				},
			},
			"command": rsyncCmdStr,
		},
	}

	return installHelmChart(e, d, releaseName, vals)
}

func formatSSHTargetHost(host string) string {
	if util.IsIPv6(host) {
		return fmt.Sprintf("[%s]", host)
	}
	return host
}
