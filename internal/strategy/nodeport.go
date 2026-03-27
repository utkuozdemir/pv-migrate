package strategy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
)

type NodePort struct{}

func (r *NodePort) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
	return runTwoReleaseStrategy(ctx, attempt, "NodePort", resolveNodePortTarget, logger)
}

func resolveNodePortTarget(
	ctx context.Context,
	attempt *migration.Attempt,
	topo topology,
	sshdRelease string,
	logger *slog.Logger,
) (sshTarget, error) {
	sshdKubeClient := topo.sshd.info.ClusterClient.KubeClient
	sshdNs := topo.sshd.info.Claim.Namespace
	svcName := sshdRelease + "-sshd"

	nodePort, err := k8s.GetNodePort(
		ctx, sshdKubeClient, sshdNs, svcName, attempt.Migration.Request.LoadBalancerTimeout,
	)
	if err != nil {
		return sshTarget{}, fmt.Errorf("failed to get NodePort: %w", err)
	}

	sshdPod, err := getSshdPodForHelmRelease(ctx, topo.sshd.info, sshdRelease)
	if err != nil {
		return sshTarget{}, fmt.Errorf("failed to get sshd pod: %w", err)
	}

	podNode := sshdPod.Spec.NodeName

	nodeIP, err := k8s.GetNodeIP(ctx, sshdKubeClient, podNode)
	if err != nil {
		logger.Warn("🔶 Could not get sshd pod's node IP, falling back to another node",
			"node", podNode, "error", err)

		nodeIP, err = k8s.GetAnyNodeIP(ctx, sshdKubeClient)
		if err != nil {
			return sshTarget{}, fmt.Errorf("failed to find usable node IP: %w", err)
		}
	} else {
		logger.Info("🔗 Using sshd pod's node for NodePort connection", "node", podNode, "ip", nodeIP)
	}

	return sshTarget{host: nodeIP, port: nodePort}, nil
}
