package strategy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/hashicorp/go-multierror"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/cli/values"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/utkuozdemir/pv-migrate/internal/console"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
)

const (
	mountStrategy        = "mount"
	clusterIPStrategy    = "clusterip"
	loadBalancerStrategy = "loadbalancer"
	localStrategy        = "local"
	nodePortStrategy     = "nodeport"

	srcMountPath  = "/source"
	destMountPath = "/dest"

	rsyncComponent = "rsync"
	sshdComponent  = "sshd"

	keyEnabled   = "enabled"
	keyNamespace = "namespace"
	keyPublicKey = "publicKey"
	keyPVCMounts = "pvcMounts"
	keyName      = "name"
	keyMountPath = "mountPath"
	keyReadOnly  = "readOnly"
	keyAffinity  = "affinity"

	rootSSHUser    = "root"
	rootSSHPort    = 22
	nonRootSSHUser = "pvmigrate"
	nonRootSSHPort = 2222
	nonRootUID     = 10000
)

var (
	nameToStrategy = map[string]Strategy{
		mountStrategy:        &Mount{},
		clusterIPStrategy:    &ClusterIP{},
		loadBalancerStrategy: &LoadBalancer{},
		localStrategy:        &Local{},
		nodePortStrategy:     &NodePort{},
	}

	helmProviders = getter.All(cli.New())

	ErrUnaccepted = errors.New("unaccepted")
)

// DeclinedError reports that a strategy cannot handle this migration, together
// with the reason it gave. It unwraps to ErrUnaccepted so the ladder's existing
// check is unaffected, and the reason stays reachable without parsing a message.
type DeclinedError struct {
	Reason string
}

func (e *DeclinedError) Error() string {
	return e.Reason + ": " + ErrUnaccepted.Error()
}

func (e *DeclinedError) Unwrap() error {
	return ErrUnaccepted
}

// Declined returns the error a strategy returns when it cannot do the job.
func Declined(reason string) error {
	return &DeclinedError{Reason: reason}
}

type Strategy interface {
	// Run runs the migration for the given task execution.
	//
	// This is the actual implementation of the migration.
	Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error
}

func GetStrategiesMapForNames(names []string) (map[string]Strategy, error) {
	sts := make(map[string]Strategy)

	for _, name := range names {
		s, ok := nameToStrategy[name]
		if !ok {
			return nil, fmt.Errorf("strategy not found: %s", name)
		}

		sts[name] = s
	}

	return sts, nil
}

func Cleanup(attempt *migration.Attempt, logger *slog.Logger) error {
	mig := attempt.Migration
	req := mig.Request

	logger.Info("🧹 Cleaning up")

	var errs error

	for _, info := range []*pvc.Info{mig.SourceInfo, mig.DestInfo} {
		for _, name := range attempt.ReleaseNames {
			err := cleanupForPVC(name, req.HelmTimeout, info)
			if err != nil {
				errs = multierror.Append(errs, err)
			}
		}
	}

	return errs
}

func cleanupForPVC(helmReleaseName string, helmUninstallTimeout time.Duration, pvcInfo *pvc.Info) error {
	ac, err := initHelmActionConfig(pvcInfo)
	if err != nil {
		return err
	}

	uninstall := action.NewUninstall(ac)
	uninstall.WaitStrategy = kube.LegacyStrategy
	uninstall.Timeout = helmUninstallTimeout
	_, err = uninstall.Run(helmReleaseName)

	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to uninstall helm release %s: %w", helmReleaseName, err)
	}

	return nil
}

func initHelmActionConfig(pvcInfo *pvc.Info) (*action.Configuration, error) {
	actionConfig := new(action.Configuration)

	err := actionConfig.Init(pvcInfo.ClusterClient.RESTClientGetter,
		pvcInfo.Claim.Namespace, os.Getenv("HELM_DRIVER"))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize helm action config: %w", err)
	}

	return actionConfig, nil
}

func sshUser(req *migration.Request) string {
	if req.NonRoot {
		return nonRootSSHUser
	}

	return rootSSHUser
}

func sshPort(req *migration.Request) int {
	if req.NonRoot {
		return nonRootSSHPort
	}

	return rootSSHPort
}

func applyNonRootValues(vals map[string]any, req *migration.Request) {
	if !req.NonRoot {
		return
	}

	nonRootSecCtx := map[string]any{
		"runAsNonRoot":             true,
		"runAsUser":                nonRootUID,
		"runAsGroup":               nonRootUID,
		"allowPrivilegeEscalation": false,
	}
	nonRootPodSecCtx := map[string]any{
		"fsGroup": nonRootUID,
	}

	for _, component := range []string{sshdComponent, rsyncComponent} {
		section, ok := vals[component].(map[string]any)
		if !ok {
			continue
		}

		section["securityContext"] = nonRootSecCtx
		section["podSecurityContext"] = nonRootPodSecCtx
	}

	if sshd, ok := vals["sshd"].(map[string]any); ok {
		sshd["containerPort"] = nonRootSSHPort
		sshd["publicKeyMountPath"] = "/home/pvmigrate/.ssh/authorized_keys"
	}
}

func getMergedHelmValues(
	baseValues map[string]any,
	request *migration.Request,
	logger *slog.Logger,
) (map[string]any, error) {
	// If an image tag is set, inject it as the lowest-priority --set values
	// so user overrides via --helm-set take precedence.
	helmValues := request.HelmValues
	if tag := request.ImageTag; tag != "" {
		imageTagValues := []string{
			"rsync.image.tag=" + tag,
			"sshd.image.tag=" + tag,
		}
		merged := make([]string, 0, len(imageTagValues)+len(helmValues))
		merged = append(merged, imageTagValues...)
		helmValues = append(merged, helmValues...)
	}

	valsOptions := values.Options{
		ValueFiles:   request.HelmValuesFiles,
		Values:       helmValues,
		StringValues: request.HelmStringValues,
		FileValues:   request.HelmFileValues,
	}

	userValues, err := valsOptions.MergeValues(helmProviders)
	if err != nil {
		return nil, fmt.Errorf("failed to merge helm values: %w", err)
	}

	// Merge using Helm's own MergeMaps: user values override base values.
	merged := loader.MergeMaps(baseValues, userValues)

	if request.ImageTag != "" {
		logger.Info("🔖 Using image tag", "tag", request.ImageTag)
	} else {
		logger.Info("🔖 Using chart default image tags")
	}

	return merged, nil
}

func installHelmChart(
	ctx context.Context,
	attempt *migration.Attempt,
	pvcInfo *pvc.Info,
	name string,
	values map[string]any,
	logger *slog.Logger,
) error {
	// Recorded before anything is installed, and here rather than reconstructed
	// later, because this is the one point every strategy passes through and the
	// only one that knows which cluster and namespace this release goes into. An
	// install that creates nothing simply yields nothing to report.
	attempt.DiagnosticTargets = append(attempt.DiagnosticTargets,
		migration.DiagnosticTarget{Release: name, Info: pvcInfo})

	helmActionConfig, err := initHelmActionConfig(pvcInfo)
	if err != nil {
		return fmt.Errorf("failed to init helm action config: %w", err)
	}

	mig := attempt.Migration

	install := action.NewInstall(helmActionConfig)
	install.Namespace = pvcInfo.Claim.Namespace
	install.ReleaseName = name
	install.WaitStrategy = kube.LegacyStrategy

	timeout, timeoutFlag := effectiveInstallTimeout(mig.Request, values)
	install.Timeout = timeout

	applyNonRootValues(values, mig.Request)

	vals, err := getMergedHelmValues(values, mig.Request, logger)
	if err != nil {
		return fmt.Errorf("failed to get merged helm values: %w", err)
	}

	// A long wait usually means the cluster already knows what is stuck, so at
	// half the budget the resources are peeked at once, turning the silent half
	// of the wait into an answer.
	stopPeek := peekAfter(timeout/2, func() {
		writeMidInstallDiagnostics(ctx, attempt, pvcInfo, name, logger)
	})
	defer stopPeek()

	if _, err = install.Run(mig.Chart, vals); err != nil {
		// The bare context error names no duration and no knob, and it is the
		// headline of every stuck-resource failure.
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf(
				"timed out after %s waiting for the release's resources to become ready (see %s): %w",
				timeout, timeoutFlag, err)
		}

		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	return nil
}

// effectiveInstallTimeout picks the install wait budget and names the flag it
// came from. The load balancer timeout only applies when this release actually
// waits for a load balancer, so a LoadBalancer-only flag cannot silently
// lengthen every other strategy's install.
func effectiveInstallTimeout(req *migration.Request, values map[string]any) (time.Duration, string) {
	if installsLoadBalancer(values) && req.LoadBalancerTimeout > req.HelmTimeout {
		return req.LoadBalancerTimeout, "--loadbalancer-timeout, which exceeds --helm-timeout"
	}

	return req.HelmTimeout, "--helm-timeout"
}

// installsLoadBalancer reports whether the values ask for a LoadBalancer
// service, which the install wait then waits on.
func installsLoadBalancer(values map[string]any) bool {
	sshd, ok := values[sshdComponent].(map[string]any)
	if !ok {
		return false
	}

	service, ok := sshd["service"].(map[string]any)
	if !ok {
		return false
	}

	return service["type"] == "LoadBalancer"
}

// peekAfter runs the peek once after the delay unless stopped first.
func peekAfter(delay time.Duration, peek func()) func() {
	done := make(chan struct{})

	go func() {
		select {
		case <-done:
		case <-time.After(delay):
			peek()
		}
	}()

	return func() { close(done) }
}

// writeMidInstallDiagnostics narrates a still-running install wait with what the
// cluster reports at that moment, on the same writer and in the same shape the
// failure block would use. Log records carry it on a structured stream.
func writeMidInstallDiagnostics(
	ctx context.Context,
	attempt *migration.Attempt,
	pvcInfo *pvc.Info,
	release string,
	logger *slog.Logger,
) {
	req := attempt.Migration.Request
	cli := pvcInfo.ClusterClient.KubeClient
	ns := pvcInfo.Claim.Namespace

	if req.StructuredLogs {
		var buf bytes.Buffer

		k8s.WriteWorkloadDiagnostics(ctx, cli, ns,
			k8s.InstanceLabelSelector(release), console.Palette{}, &buf, logger)
		logger.Warn("🔶 Still waiting for the release's resources; what the cluster reports so far",
			"release", release, "namespace", ns, "diagnostics", buf.String())

		return
	}

	palette := console.Palette{Enabled: req.ColorOutput}

	fmt.Fprintf(req.Writer, "\n%s\n\n  %s (namespace %s):\n",
		palette.Bold("Still waiting; what the cluster reports so far:"), release, ns)
	k8s.WriteWorkloadDiagnostics(ctx, cli, ns,
		k8s.InstanceLabelSelector(release), palette, req.Writer, logger)
	fmt.Fprintln(req.Writer)
}
