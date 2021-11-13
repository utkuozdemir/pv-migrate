package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"helm.sh/helm/v3/pkg/action"
	"time"
)

type Mnt2 struct {
}

func (r *Mnt2) canDo(t *task.Task) bool {
	s := t.SourceInfo
	d := t.DestInfo
	sameCluster := s.ClusterClient == d.ClusterClient
	if !sameCluster {
		return false
	}

	sameNamespace := s.Claim.Namespace == d.Claim.Namespace
	if !sameNamespace {
		return false
	}

	sameNode := s.MountedNode == d.MountedNode
	return sameNode || s.SupportsROX || s.SupportsRWX || d.SupportsRWX
}

func (r *Mnt2) Run(e *task.Execution) (bool, error) {
	t := e.Task
	if !r.canDo(t) {
		return false, nil
	}

	s := e.Task.SourceInfo
	d := e.Task.DestInfo

	ns := s.Claim.Namespace

	helmActionConfig, err := initHelmActionConfig(e.Logger, e.Task.SourceInfo)
	if err != nil {
		return true, err
	}

	install := action.NewInstall(helmActionConfig)
	install.Namespace = ns
	install.ReleaseName = e.HelmReleaseName
	install.Wait = true
	install.Timeout = 1 * time.Minute

	node := determineTargetNode(t)

	opts := t.Migration.Options
	vals := map[string]interface{}{
		"rsync": map[string]interface{}{
			"enabled":               true,
			"nodeName":              node,
			"mountSource":           true,
			"deleteExtraneousFiles": opts.DeleteExtraneousFiles,
			"noChown":               opts.NoChown,
		},
		"source": map[string]interface{}{
			"namespace":        ns,
			"pvcName":          s.Claim.Name,
			"pvcMountReadOnly": opts.SourceMountReadOnly,
			"path":             t.Migration.Source.Path,
		},
		"dest": map[string]interface{}{
			"namespace": ns,
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
	err = k8s.WaitUntilJobIsCompleted(e.Logger, kubeClient, ns, jobName, showProgressBar)
	return true, err
}

func determineTargetNode(t *task.Task) string {
	s := t.SourceInfo
	d := t.DestInfo
	if (s.SupportsROX || s.SupportsRWX) && d.SupportsRWX {
		return ""
	}
	if !s.SupportsROX && !s.SupportsRWX {
		return s.MountedNode
	}
	return d.MountedNode
}
