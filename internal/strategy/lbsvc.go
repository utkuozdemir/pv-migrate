package strategy

import (
	"fmt"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/internal/util"
	"github.com/utkuozdemir/pv-migrate/migration"
)

type LbSvc struct{}

func (r *LbSvc) Run(a *migration.Attempt) (bool, error) {
	m := a.Migration

	s := m.SourceInfo
	d := m.DestInfo
	sourceNs := s.Claim.Namespace
	destNs := d.Claim.Namespace

	m.Logger.Info(":key: Generating SSH key pair")
	keyAlgorithm := m.Request.KeyAlgorithm
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

	err = installOnSource(a, srcReleaseName, publicKey, srcMountPath)
	if err != nil {
		return true, err
	}

	sourceKubeClient := a.Migration.SourceInfo.ClusterClient.KubeClient
	svcName := srcReleaseName + "-sshd"
	lbSvcAddress, err := k8s.GetServiceAddress(sourceKubeClient, sourceNs, svcName)
	if err != nil {
		return true, err
	}

	sshTargetHost := formatSSHTargetHost(lbSvcAddress)
	if m.Request.DestHostOverride != "" {
		sshTargetHost = m.Request.DestHostOverride
	}

	err = installOnDest(a, destReleaseName, privateKey, privateKeyMountPath,
		sshTargetHost, srcMountPath, destMountPath)
	if err != nil {
		return true, err
	}

	showProgressBar := !a.Migration.Request.NoProgressBar
	kubeClient := d.ClusterClient.KubeClient
	jobName := destReleaseName + "-rsync"
	err = k8s.WaitForJobCompletion(a.Logger, kubeClient, destNs, jobName, showProgressBar)
	return true, err
}

func installOnSource(a *migration.Attempt, releaseName, publicKey, srcMountPath string) error {
	t := a.Migration
	s := t.SourceInfo
	ns := s.Claim.Namespace

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
					"readOnly":  t.Request.SourceMountReadOnly,
					"mountPath": srcMountPath,
				},
			},
		},
	}

	return installHelmChart(a, s, releaseName, vals)
}

func installOnDest(a *migration.Attempt, releaseName, privateKey,
	privateKeyMountPath, sshHost, srcMountPath, destMountPath string,
) error {
	t := a.Migration
	d := t.DestInfo
	ns := d.Claim.Namespace

	srcPath := srcMountPath + "/" + t.Request.Source.Path
	destPath := destMountPath + "/" + t.Request.Dest.Path
	rsyncCmd := rsync.Cmd{
		NoChown:    t.Request.NoChown,
		Delete:     t.Request.DeleteExtraneousFiles,
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

	return installHelmChart(a, d, releaseName, vals)
}

func formatSSHTargetHost(host string) string {
	if util.IsIPv6(host) {
		return fmt.Sprintf("[%s]", host)
	}
	return host
}
