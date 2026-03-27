package strategy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/util"
)

type LoadBalancer struct{}

func (r *LoadBalancer) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
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

func formatSSHTargetHost(host string) string {
	if util.IsIPv6(host) {
		return fmt.Sprintf("[%s]", host)
	}

	return host
}
