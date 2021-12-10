package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/internal/task"
)

type Svc struct {
}

func (r *Svc) canDo(t *task.Task) bool {
	s := t.SourceInfo
	d := t.DestInfo
	sameCluster := s.ClusterClient.RestConfig.Host == d.ClusterClient.RestConfig.Host
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

	srcPath := srcMountPath + "/" + t.Migration.Source.Path
	destPath := destMountPath + "/" + t.Migration.Dest.Path
	rsyncCmd := rsync.Cmd{
		NoChown:    opts.NoChown,
		Delete:     opts.DeleteExtraneousFiles,
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
					"readOnly":  opts.SourceMountReadOnly,
				},
			},
		},
	}

	doneCh := registerCleanupHook(e, releaseNames)
	defer cleanupAndReleaseHook(e, releaseNames, doneCh)

	err = installHelmChart(e, d, releaseName, vals)
	if err != nil {
		return true, err
	}

	showProgressBar := !opts.NoProgressBar
	kubeClient := t.SourceInfo.ClusterClient.KubeClient
	jobName := releaseName + "-rsync"
	err = k8s.WaitForJobCompletion(e.Logger, kubeClient, destNs, jobName, showProgressBar)
	return true, err
}
