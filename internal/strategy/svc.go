package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"helm.sh/helm/v3/pkg/action"
	"time"
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

	helmActionConfig, err := initHelmActionConfig(e.Logger, e.Task.DestInfo)
	if err != nil {
		return true, err
	}

	install := action.NewInstall(helmActionConfig)
	install.Namespace = destNs
	install.ReleaseName = e.HelmReleaseName
	install.Wait = true
	install.Timeout = 1 * time.Minute

	t.Logger.Info(":key: Generating SSH key pair")
	keyAlgorithm := t.Migration.Options.KeyAlgorithm
	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return true, err
	}
	privateKeyMountPath := "/root/.ssh/id_" + keyAlgorithm

	opts := t.Migration.Options
	vals := map[string]interface{}{
		"rsync": map[string]interface{}{
			"enabled":               true,
			"deleteExtraneousFiles": opts.DeleteExtraneousFiles,
			"noChown":               opts.NoChown,
			"privateKeyMount":       true,
			"privateKey":            privateKey,
			"privateKeyMountPath":   privateKeyMountPath,
		},
		"sshd": map[string]interface{}{
			"enabled":   true,
			"publicKey": publicKey,
		},
		"source": map[string]interface{}{
			"namespace":        sourceNs,
			"pvcName":          s.Claim.Name,
			"pvcMountReadOnly": opts.SourceMountReadOnly,
			"path":             t.Migration.Source.Path,
		},
		"dest": map[string]interface{}{
			"namespace": destNs,
			"pvcName":   d.Claim.Name,
			"path":      t.Migration.Dest.Path,
		},
	}

	_, err = install.Run(t.Chart, vals)
	if err != nil {
		return true, err
	}

	doneCh := registerCleanupHook(e)
	defer cleanupAndReleaseHook(e, doneCh)

	showProgressBar := !opts.NoProgressBar
	kubeClient := t.SourceInfo.ClusterClient.KubeClient
	jobName := e.HelmReleaseName + "-rsync"
	err = k8s.WaitUntilJobIsCompleted(e.Logger, kubeClient, destNs, jobName, showProgressBar)
	return true, err
}
