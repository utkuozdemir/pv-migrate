package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/internal/util"
)

type LoadBalancer struct{}

// SharedSource holds the result of setting up a shared source sshd endpoint
// that can serve multiple transfers in batch mode.
type SharedSource struct {
	// Address is the SSH target host (formatted for SSH use).
	Address string
	// ReleaseName is the Helm release name of the shared source endpoint.
	ReleaseName string
	// PrivateKey is the SSH private key for connecting to the shared source.
	PrivateKey string
	// KeyAlgorithm is the SSH key algorithm used.
	KeyAlgorithm string
	// MountPaths maps source PVC name to its mount path on the sshd pod.
	MountPaths map[string]string
}

// SetupSharedSource installs a single sshd deployment + LoadBalancer service
// that mounts ALL given source PVCs. This is used in batch mode to avoid
// creating one LB service per transfer.
func (r *LoadBalancer) SetupSharedSource(
	ctx context.Context,
	attempt *migration.Attempt,
	allSourceInfos []*pvc.Info,
	readWrite bool,
	logger *slog.Logger,
) (*SharedSource, error) {
	keyAlgorithm := attempt.Migration.Request.KeyAlgorithm

	logger.Info("🔑 Generating SSH key pair for shared source endpoint", "algorithm", keyAlgorithm)

	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return nil, fmt.Errorf("failed to create ssh key pair: %w", err)
	}

	releaseName := attempt.HelmReleaseNamePrefix + "-shared-src"

	// Build PVC mounts for all source PVCs.
	mountPaths := make(map[string]string, len(allSourceInfos))
	pvcMounts := make([]map[string]any, 0, len(allSourceInfos))

	for _, info := range allSourceInfos {
		mountPath := SrcMountPath + "/" + info.Claim.Name
		mountPaths[info.Claim.Name] = mountPath
		pvcMounts = append(pvcMounts, map[string]any{
			"name":      info.Claim.Name,
			"readOnly":  !readWrite,
			"mountPath": mountPath,
		})
	}

	// Use first PVC's info for namespace, client, and affinity.
	firstInfo := allSourceInfos[0]

	vals := map[string]any{
		"sshd": map[string]any{
			"enabled":   true,
			"namespace": firstInfo.Claim.Namespace,
			"publicKey": publicKey,
			"service": map[string]any{
				"type": "LoadBalancer",
			},
			"pvcMounts": pvcMounts,
			"affinity":  firstInfo.AffinityHelmValues,
		},
	}

	logger.Info("📦 Installing shared source sshd with all PVCs",
		"release", releaseName, "pvc_count", len(allSourceInfos))

	if err := installHelmChart(attempt, firstInfo, releaseName, vals, logger); err != nil {
		return nil, fmt.Errorf("failed to install shared source: %w", err)
	}

	sourceKubeClient := firstInfo.ClusterClient.KubeClient
	svcName := releaseName + "-sshd"

	lbAddress, err := k8s.GetServiceAddress(
		ctx,
		sourceKubeClient,
		firstInfo.Claim.Namespace,
		svcName,
		attempt.Migration.Request.LoadBalancerTimeout,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get shared source service address: %w", err)
	}

	sshTargetHost := formatSSHTargetHost(lbAddress)

	logger.Info("🌐 Shared source endpoint ready", "address", sshTargetHost)

	return &SharedSource{
		Address:      sshTargetHost,
		ReleaseName:  releaseName,
		PrivateKey:   privateKey,
		KeyAlgorithm: keyAlgorithm,
		MountPaths:   mountPaths,
	}, nil
}

// CleanupSharedSource cleans up a shared source endpoint.
func (r *LoadBalancer) CleanupSharedSource(
	pvcInfo *pvc.Info,
	releaseName string,
	helmTimeout time.Duration,
	logger *slog.Logger,
) {
	logger.Info("🧹 Cleaning up shared source endpoint", "release", releaseName)

	if err := cleanupForPVC(releaseName, helmTimeout, pvcInfo); err != nil {
		logger.Warn("🔶 Failed to clean up shared source endpoint", "error", err)
	}
}

func (r *LoadBalancer) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
	// If a shared source endpoint is provided (batch mode), skip source install.
	if ep := attempt.SourceEndpoint; ep != nil {
		return r.runWithSharedSource(ctx, attempt, ep, logger)
	}

	return runTwoReleaseStrategy(ctx, attempt, "LoadBalancer", resolveLBTarget, logger)
}

func resolveLBTarget(
	ctx context.Context,
	attempt *migration.Attempt,
	topo topology,
	sshdRelease string,
	_ *slog.Logger,
) (sshTarget, error) {
	svcName := sshdRelease + "-sshd"

	lbAddress, err := k8s.GetServiceAddress(
		ctx,
		topo.sshd.info.ClusterClient.KubeClient,
		topo.sshd.info.Claim.Namespace,
		svcName,
		attempt.Migration.Request.LoadBalancerTimeout,
	)
	if err != nil {
		return sshTarget{}, fmt.Errorf("failed to get service address: %w", err)
	}

	return sshTarget{host: formatSSHTargetHost(lbAddress)}, nil
}

// runWithSharedSource runs a single transfer using a pre-existing shared source endpoint.
// Only the destination rsync job is created; the source sshd is already running.
func (r *LoadBalancer) runWithSharedSource(
	ctx context.Context,
	attempt *migration.Attempt,
	ep *migration.SourceEndpoint,
	logger *slog.Logger,
) error {
	mig := attempt.Migration
	destInfo := mig.DestInfo

	privateKeyMountPath := "/tmp/id_" + ep.KeyAlgorithm

	destReleaseName := attempt.HelmReleaseNamePrefix + "-dest"
	attempt.ReleaseNames = []string{destReleaseName}

	sshTargetHost := ep.Address
	if mig.Request.DestHostOverride != "" {
		sshTargetHost = mig.Request.DestHostOverride
	}

	srcPath := ep.SrcMountPath + "/" + mig.Request.Source.Path
	destPath := DestMountPath + "/" + mig.Request.Dest.Path
	rsyncCmd := rsync.Cmd{
		NoChown:    mig.Request.NoChown,
		NonRoot:    mig.Request.NonRoot,
		Delete:     mig.Request.DeleteExtraneousFiles,
		SrcPath:    srcPath,
		DestPath:   destPath,
		SrcUseSSH:  true,
		SrcSSHHost: sshTargetHost,
		SrcSSHUser: sshUser(mig.Request),
		Compress:   !mig.Request.NoCompress,
		ExtraArgs:  mig.Request.RsyncExtraArgs,
	}

	rsyncCmdStr, err := rsyncCmd.Build()
	if err != nil {
		return fmt.Errorf("failed to build rsync command: %w", err)
	}

	rsyncSide := componentSide{info: destInfo, mountPath: DestMountPath}
	rsyncVals := buildRsyncHelmValues(rsyncSide, rsyncCmdStr, ep.PrivateKey, privateKeyMountPath)
	rsyncVals["sshRemoteHost"] = sshTargetHost

	if err = installHelmChart(attempt, destInfo, destReleaseName, map[string]any{"rsync": rsyncVals}, logger); err != nil {
		return fmt.Errorf("failed to install on dest: %w", err)
	}

	return waitForRsyncJob(ctx, attempt, destInfo, destReleaseName, logger)
}

func formatSSHTargetHost(host string) string {
	if util.IsIPv6(host) {
		return fmt.Sprintf("[%s]", host)
	}

	return host
}

// BatchTransferInfo describes a single PVC pair within a batch transfer.
type BatchTransferInfo struct {
	// SourceMountPath is the mount path of this source PVC on the shared sshd pod.
	SourceMountPath string
	// DestInfo is the PVC info for the destination PVC.
	DestInfo *pvc.Info
	// DestMountPath is the mount path for this dest PVC on the batch rsync pod.
	DestMountPath string
	// Request is the migration request for this specific PVC pair.
	Request *migration.Request
}

// RunBatchTransfer installs a single rsync job that mounts ALL dest PVCs
// and runs a compound rsync command covering every (src, dest) pair.
// This achieves the "1 sshd <-> 1 rsync" pattern per namespace.
func (r *LoadBalancer) RunBatchTransfer(
	ctx context.Context,
	attempt *migration.Attempt,
	sshHost string,
	privateKey string,
	keyAlgorithm string,
	transfers []BatchTransferInfo,
	logger *slog.Logger,
) error {
	if len(transfers) == 0 {
		return fmt.Errorf("no transfers provided for batch")
	}

	privateKeyMountPath := "/tmp/id_" + keyAlgorithm

	destReleaseName := attempt.HelmReleaseNamePrefix + "-batch-dest"
	attempt.ReleaseNames = []string{destReleaseName}

	// Use the first transfer's request as representative for common settings.
	firstReq := transfers[0].Request

	if firstReq.DestHostOverride != "" {
		sshHost = firstReq.DestHostOverride
	}

	// Build compound rsync command for all pairs.
	batchCmd := rsync.Cmd{
		NoChown:    firstReq.NoChown,
		Delete:     firstReq.DeleteExtraneousFiles,
		SrcUseSSH:  true,
		SrcSSHHost: sshHost,
		Compress:   !firstReq.NoCompress,
	}

	entries := make([]rsync.BatchEntry, 0, len(transfers))
	for _, t := range transfers {
		srcPath := t.SourceMountPath + "/" + t.Request.Source.Path
		destPath := t.DestMountPath + "/" + t.Request.Dest.Path
		entries = append(entries, rsync.BatchEntry{SrcPath: srcPath, DestPath: destPath})
	}

	rsyncCmdStr, err := batchCmd.BuildBatch(entries)
	if err != nil {
		return fmt.Errorf("failed to build batch rsync command: %w", err)
	}

	// Build pvcMounts for ALL dest PVCs.
	pvcMounts := make([]map[string]any, 0, len(transfers))
	for _, t := range transfers {
		pvcMounts = append(pvcMounts, map[string]any{
			"name":      t.DestInfo.Claim.Name,
			"mountPath": t.DestMountPath,
		})
	}

	// Use the first transfer's dest info for Helm install context (namespace, client).
	firstDestInfo := transfers[0].DestInfo

	vals := map[string]any{
		"rsync": map[string]any{
			"enabled":             true,
			"namespace":           firstDestInfo.Claim.Namespace,
			"privateKeyMount":     true,
			"privateKey":          privateKey,
			"privateKeyMountPath": privateKeyMountPath,
			"sshRemoteHost":       sshHost,
			"pvcMounts":           pvcMounts,
			"command":             rsyncCmdStr,
			"affinity":            firstDestInfo.AffinityHelmValues,
		},
	}

	logger.Info("📦 Installing batch rsync job with all dest PVCs",
		"release", destReleaseName, "pvc_count", len(transfers))

	if err := installHelmChart(attempt, firstDestInfo, destReleaseName, vals, logger); err != nil {
		return fmt.Errorf("failed to install batch rsync: %w", err)
	}

	// Wait for the job to finish.
	showProgressBar := firstReq.ShowProgressBar
	kubeClient := firstDestInfo.ClusterClient.KubeClient
	jobName := destReleaseName + "-rsync"

	if err := k8s.WaitForJobCompletion(
		ctx,
		kubeClient,
		firstDestInfo.Claim.Namespace,
		jobName,
		showProgressBar,
		firstReq.Writer,
		logger,
	); err != nil {
		return fmt.Errorf("failed to wait for batch rsync job completion: %w", err)
	}

	return nil
}
