package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/migration"
)

type Svc struct{}

func (r *Svc) canDo(t *migration.Migration) bool {
	s := t.SourceInfo
	d := t.DestInfo
	sameCluster := s.ClusterClient.RestConfig.Host == d.ClusterClient.RestConfig.Host
	return sameCluster
}

func (r *Svc) Run(a *migration.Attempt) (bool, error) {
	m := a.Migration
	if !r.canDo(m) {
		return false, nil
	}

	s := a.Migration.SourceInfo
	d := a.Migration.DestInfo
	sourceNs := s.Claim.Namespace
	destNs := d.Claim.Namespace

	m.Logger.Info(":key: Generating SSH key pair")
	keyAlgorithm := m.Request.KeyAlgorithm
	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return true, err
	}
	privateKeyMountPath := "/root/.ssh/id_" + keyAlgorithm

	releaseName := a.HelmReleaseNamePrefix
	releaseNames := []string{releaseName}

	sshRemoteHost := releaseName + "-sshd." + sourceNs

	srcMountPath := "/source"
	destMountPath := "/dest"

	srcPath := srcMountPath + "/" + m.Request.Source.Path
	destPath := destMountPath + "/" + m.Request.Dest.Path
	rsyncCmd := rsync.Cmd{
		NoChown:    m.Request.NoChown,
		Delete:     m.Request.DeleteExtraneousFiles,
		SrcPath:    srcPath,
		DestPath:   destPath,
		SrcUseSsh:  true,
		SrcSshHost: sshRemoteHost,
	}
	rsyncCmdStr, err := rsyncCmd.Build()
	if err != nil {
		return true, err
	}

	vals := map[string]interface{}{
		"rsync": map[string]interface{}{
			"enabled":             true,
			"namespace":           destNs,
			"privateKeyMount":     true,
			"privateKey":          privateKey,
			"privateKeyMountPath": privateKeyMountPath,
			"pvcMounts": []map[string]interface{}{
				{
					"name":      d.Claim.Name,
					"mountPath": destMountPath,
				},
			},
			"command": rsyncCmdStr,
		},
		"sshd": map[string]interface{}{
			"enabled":   true,
			"namespace": sourceNs,
			"publicKey": publicKey,
			"pvcMounts": []map[string]interface{}{
				{
					"name":      s.Claim.Name,
					"mountPath": srcMountPath,
					"readOnly":  m.Request.SourceMountReadOnly,
				},
			},
		},
	}

	doneCh := registerCleanupHook(a, releaseNames)
	defer cleanupAndReleaseHook(a, releaseNames, doneCh)

	err = installHelmChart(a, d, releaseName, vals)
	if err != nil {
		return true, err
	}

	showProgressBar := !m.Request.NoProgressBar
	kubeClient := m.SourceInfo.ClusterClient.KubeClient
	jobName := releaseName + "-rsync"
	err = k8s.WaitForJobCompletion(a.Logger, kubeClient, destNs, jobName, showProgressBar)
	return true, err
}
