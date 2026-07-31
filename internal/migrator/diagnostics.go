package migrator

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/internal/console"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
)

// collectDiagnostics asks every cluster this attempt installed into what it has
// to say about the resources it created. It runs while a migration is already
// failing, so it is best effort throughout: whatever cannot be read is left out
// rather than turned into a second error.
func collectDiagnostics(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) string {
	var buf bytes.Buffer

	palette := console.Palette{Enabled: attempt.Migration.Request.ColorOutput}

	for _, target := range attempt.DiagnosticTargets {
		info := target.Info
		if info == nil || info.Claim == nil || info.ClusterClient == nil || info.ClusterClient.KubeClient == nil {
			continue
		}

		fmt.Fprintf(&buf, "  %s (%snamespace %s):\n", target.Release, sideLabel(attempt, info), info.Claim.Namespace)

		k8s.WriteWorkloadDiagnostics(ctx, info.ClusterClient.KubeClient, info.Claim.Namespace,
			k8s.InstanceLabelSelector(target.Release), palette, &buf, logger)
	}

	return buf.String()
}

// sideLabel names which side of the migration a diagnostic target belongs to,
// but only when the two sides actually differ: two blocks distinguished by
// nothing but a namespace make the reader do the mapping the tool already has.
func sideLabel(attempt *migration.Attempt, info *pvc.Info) string {
	mig := attempt.Migration
	if mig == nil || !sidesDiffer(mig.SourceInfo, mig.DestInfo) {
		return ""
	}

	switch info {
	case mig.SourceInfo:
		return "source side, "
	case mig.DestInfo:
		return "destination side, "
	default:
		return ""
	}
}

func sidesDiffer(src, dst *pvc.Info) bool {
	if src == nil || dst == nil || src.Claim == nil || dst.Claim == nil ||
		src.ClusterClient == nil || dst.ClusterClient == nil {
		return false
	}

	return src.Claim.Namespace != dst.Claim.Namespace ||
		src.ClusterClient.RestConfig.Host != dst.ClusterClient.RestConfig.Host
}
