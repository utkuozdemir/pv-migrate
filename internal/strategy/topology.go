package strategy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
)

// topology maps sshd and rsync components to source/dest sides.
// In pull mode (default), sshd runs on the source side and rsync on the dest side.
// In push mode (--rsync-push), sshd runs on the dest side and rsync on the source side.
type topology struct {
	sshd  componentSide
	rsync componentSide
	push  bool
}

// componentSide describes which cluster/PVC a component is deployed on.
type componentSide struct {
	info      *pvc.Info
	mountPath string
	readOnly  bool
}

// sshTarget holds the resolved SSH connection endpoint for two-release strategies.
type sshTarget struct {
	host string
	port int
}

func resolveTopology(mig *migration.Migration) topology {
	src := mig.SourceInfo
	dst := mig.DestInfo
	srcReadOnly := !mig.Request.SourceMountReadWrite

	if mig.Request.Push {
		return topology{
			push:  true,
			sshd:  componentSide{info: dst, mountPath: destMountPath, readOnly: false},
			rsync: componentSide{info: src, mountPath: srcMountPath, readOnly: srcReadOnly},
		}
	}

	return topology{
		push:  false,
		sshd:  componentSide{info: src, mountPath: srcMountPath, readOnly: srcReadOnly},
		rsync: componentSide{info: dst, mountPath: destMountPath, readOnly: false},
	}
}

// releaseNames returns sshd and rsync Helm release names for two-release strategies.
// The suffix reflects which cluster each release is installed on (-src or -dest).
func (t topology) releaseNames(prefix string) [2]string {
	if t.push {
		return [2]string{prefix + "-dest", prefix + "-src"}
	}

	return [2]string{prefix + "-src", prefix + "-dest"}
}

func generateSSHKeys(keyAlgorithm string, logger *slog.Logger) (string, string, string, error) {
	logger.Info("🔑 Generating SSH key pair", "algorithm", keyAlgorithm)

	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create ssh key pair: %w", err)
	}

	return publicKey, privateKey, "/tmp/id_" + keyAlgorithm, nil
}

type resolveTargetFunc func(ctx context.Context, attempt *migration.Attempt,
	topo topology, sshdRelease string, _ *slog.Logger) (sshTarget, error)

// runTwoReleaseStrategy runs a two-release (sshd + rsync) migration.
// The resolveTarget callback is called after sshd is installed to determine the SSH endpoint.
func runTwoReleaseStrategy(
	ctx context.Context,
	attempt *migration.Attempt,
	serviceType string,
	resolveTarget resolveTargetFunc,
	logger *slog.Logger,
) error {
	mig := attempt.Migration
	topo := resolveTopology(mig)

	publicKey, privateKey, privateKeyMountPath, err := generateSSHKeys(mig.Request.KeyAlgorithm, logger)
	if err != nil {
		return err
	}

	releases := topo.releaseNames(attempt.HelmReleaseNamePrefix)
	sshdRelease, rsyncRelease := releases[0], releases[1]
	releaseNames := releases[:]

	doneCh := registerCleanupHook(attempt, releaseNames, logger)
	defer cleanupAndReleaseHook(ctx, attempt, releaseNames, doneCh, logger)

	if err = installSshd(attempt, topo, sshdRelease, publicKey, serviceType, logger); err != nil {
		return fmt.Errorf("failed to install sshd: %w", err)
	}

	target, err := resolveTarget(ctx, attempt, topo, sshdRelease, logger)
	if err != nil {
		return err
	}

	sshTargetHost := target.host
	if mig.Request.DestHostOverride != "" {
		sshTargetHost = mig.Request.DestHostOverride
	}

	if err = installRsyncJob(attempt, topo, rsyncRelease, privateKey, privateKeyMountPath,
		sshTargetHost, target.port, logger); err != nil {
		return fmt.Errorf("failed to install rsync job: %w", err)
	}

	return waitForRsyncJob(ctx, attempt, topo.rsync.info, rsyncRelease, logger)
}

func buildRsyncCmd(req *migration.Request, push bool, sshHost string, port int) rsync.Cmd {
	cmd := rsync.Cmd{
		Port:      port,
		NoChown:   req.NoChown,
		NonRoot:   req.NonRoot,
		Delete:    req.DeleteExtraneousFiles,
		SrcPath:   srcMountPath + "/" + req.Source.Path,
		DestPath:  destMountPath + "/" + req.Dest.Path,
		Compress:  !req.NoCompress,
		ExtraArgs: req.RsyncExtraArgs,
	}

	if push {
		cmd.DestUseSSH = true
		cmd.DestSSHHost = sshHost
		cmd.DestSSHUser = sshUser(req)
	} else {
		cmd.SrcUseSSH = true
		cmd.SrcSSHHost = sshHost
		cmd.SrcSSHUser = sshUser(req)
	}

	return cmd
}

func buildSshdHelmValues(side componentSide, publicKey string) map[string]any {
	return map[string]any{
		"enabled":   true,
		"namespace": side.info.Claim.Namespace,
		"publicKey": publicKey,
		"pvcMounts": []map[string]any{
			{
				"name":      side.info.Claim.Name,
				"readOnly":  side.readOnly,
				"mountPath": side.mountPath,
			},
		},
		"affinity": side.info.AffinityHelmValues,
	}
}

func buildRsyncHelmValues(side componentSide, rsyncCmd, privateKey, privateKeyMountPath string) map[string]any {
	return map[string]any{
		"enabled":             true,
		"namespace":           side.info.Claim.Namespace,
		"privateKeyMount":     true,
		"privateKey":          privateKey,
		"privateKeyMountPath": privateKeyMountPath,
		"pvcMounts": []map[string]any{
			{
				"name":      side.info.Claim.Name,
				"mountPath": side.mountPath,
				"readOnly":  side.readOnly,
			},
		},
		"command":  rsyncCmd,
		"affinity": side.info.AffinityHelmValues,
	}
}

func installSshd(
	attempt *migration.Attempt,
	topo topology,
	releaseName, publicKey, serviceType string,
	logger *slog.Logger,
) error {
	sshdVals := buildSshdHelmValues(topo.sshd, publicKey)
	sshdVals["service"] = map[string]any{"type": serviceType}

	return installHelmChart(attempt, topo.sshd.info, releaseName, map[string]any{"sshd": sshdVals}, logger)
}

func installRsyncJob(
	attempt *migration.Attempt,
	topo topology,
	releaseName, privateKey, privateKeyMountPath, sshHost string,
	sshPort int,
	logger *slog.Logger,
) error {
	mig := attempt.Migration

	rsyncCmd := buildRsyncCmd(mig.Request, topo.push, sshHost, sshPort)

	rsyncCmdStr, err := rsyncCmd.Build()
	if err != nil {
		return fmt.Errorf("failed to build rsync command: %w", err)
	}

	rsyncVals := buildRsyncHelmValues(topo.rsync, rsyncCmdStr, privateKey, privateKeyMountPath)
	rsyncVals["sshRemoteHost"] = sshHost

	if sshPort != 0 {
		rsyncVals["sshRemotePort"] = sshPort
	}

	return installHelmChart(attempt, topo.rsync.info, releaseName, map[string]any{"rsync": rsyncVals}, logger)
}

func waitForRsyncJob(
	ctx context.Context,
	attempt *migration.Attempt,
	rsyncInfo *pvc.Info,
	rsyncRelease string,
	logger *slog.Logger,
) error {
	mig := attempt.Migration
	kubeClient := rsyncInfo.ClusterClient.KubeClient
	namespace := rsyncInfo.Claim.Namespace
	jobName := rsyncRelease + "-rsync"

	if mig.Request.Detach {
		if _, err := k8s.WaitForJobStart(ctx, kubeClient, namespace, jobName, logger); err != nil {
			return fmt.Errorf("failed to wait for job to start: %w", err)
		}

		attempt.Detached = true

		return nil
	}

	if err := k8s.WaitForJobCompletion(
		ctx, kubeClient, namespace, jobName,
		mig.Request.ShowProgressBar, mig.Request.Writer, logger,
	); err != nil {
		return fmt.Errorf("failed to wait for job completion: %w", err)
	}

	return nil
}
