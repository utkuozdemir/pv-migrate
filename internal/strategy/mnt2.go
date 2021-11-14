package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"helm.sh/helm/v3/pkg/action"
	"strconv"
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

	helmValues := []string{
		"rsync.enabled=true",
		"rsync.nodeName=" + node,
		"rsync.mountSource=true",
		"rsync.deleteExtraneousFiles=" + strconv.FormatBool(opts.DeleteExtraneousFiles),
		"rsync.noChown=" + strconv.FormatBool(opts.NoChown),
		"source.namespace=" + ns,
		"source.pvcName=" + s.Claim.Name,
		"source.pvcMountReadOnly=" + strconv.FormatBool(opts.NoChown),
		"source.path=" + t.Migration.Source.Path,
		"dest.namespace=" + ns,
		"dest.pvcName=" + d.Claim.Name,
		"dest.path=" + t.Migration.Dest.Path,
	}

	vals, err := getMergedHelmValues(helmValues, opts)
	if err != nil {
		return true, err
	}

	doneCh := registerCleanupHook(e)
	defer cleanupAndReleaseHook(e, doneCh)

	_, err = install.Run(t.Chart, vals)
	if err != nil {
		return true, err
	}

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
