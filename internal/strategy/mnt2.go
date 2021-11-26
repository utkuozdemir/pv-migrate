package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"strconv"
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
	opts := t.Migration.Options

	node := determineTargetNode(t)

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

	releaseName := e.HelmReleaseNamePrefix
	releaseNames := []string{releaseName}

	doneCh := registerCleanupHook(e, releaseNames)
	defer cleanupAndReleaseHook(e, releaseNames, doneCh)

	err := installHelmChart(e, s, "", helmValues)
	if err != nil {
		return true, err
	}

	showProgressBar := !opts.NoProgressBar
	kubeClient := t.SourceInfo.ClusterClient.KubeClient
	jobName := e.HelmReleaseNamePrefix + "-rsync"
	err = k8s.WaitForJobCompletion(e.Logger, kubeClient, ns, jobName, showProgressBar)
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
