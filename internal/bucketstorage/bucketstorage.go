package bucketstorage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"helm.sh/helm/v4/pkg/action"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/cli/values"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/kube"
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/internal/console"
	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/opid"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/rclone"
)

const (
	dataMountPath = "/data"
	// Keep in sync with the pvmigrate user created in docker/rclone/Dockerfile.
	nonRootUID = 10000
)

// safeBucketSegment matches strings that are safe for use in bucket paths:
// alphanumeric, hyphens, underscores, and dots.
var safeBucketSegment = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

var helmProviders = getter.All(cli.New())

// Request holds all parameters for a backup or restore operation.
type Request struct {
	ID                    string
	ImageTag              string
	ChartVersion          string
	Direction             string // rclone.DirectionBackup or rclone.DirectionRestore
	KubeconfigPath        string
	Context               string
	Namespace             string
	PVCName               string
	IgnoreMounted         bool
	NonRoot               bool
	Detach                bool
	NoCleanup             bool
	NoCleanupOnFailure    bool
	DeleteExtraneousFiles bool

	// Bucket storage config
	Backend               string
	Bucket                string
	S3Provider            string
	Endpoint              string
	Region                string
	AccessKey             string
	SecretKey             string
	StorageAccount        string
	StorageKey            string
	GCSServiceAccountJSON string
	GCSBucketPolicyOnly   *bool
	Name                  string
	Prefix                string
	Path                  string
	RcloneConfigFile      string
	Remote                string
	RcloneExtraArgs       string

	HelmTimeout      time.Duration
	HelmValuesFiles  []string
	HelmValues       []string
	HelmFileValues   []string
	HelmStringValues []string

	Writer io.Writer
	Logger *slog.Logger

	// StructuredLogs reports that the logger writes machine-readable records to
	// the same stream as Writer. Plain-text blocks are suppressed then, and the
	// same information is emitted as log records instead.
	StructuredLogs bool

	// ColorOutput colors the plain-text report blocks semantically. Set only
	// when the writer is a terminal and the logs are not machine-readable.
	ColorOutput bool
}

// Run executes a backup or restore operation.
//
//nolint:cyclop,funlen
func Run(ctx context.Context, req *Request) error {
	logger := req.Logger

	// Only the public API defaults the writer, so a direct caller can leave it
	// unset. Everything below writes to it without checking.
	if req.Writer == nil {
		req.Writer = io.Discard
	}

	operationID := req.ID
	if operationID == "" {
		operationID = opid.Generate()
	}

	logger = logger.With("id", operationID, "direction", req.Direction)

	rcloneConf, err := buildRcloneConfig(req)
	if err != nil {
		return fmt.Errorf("failed to build rclone config: %w", err)
	}

	remotePath, err := buildRemotePath(req)
	if err != nil {
		return err
	}

	localPath := dataMountPath

	if req.Path != "" {
		if err = validateSubpath(req.Path); err != nil {
			return fmt.Errorf("invalid --path: %w", err)
		}

		localPath = path.Join(dataMountPath, req.Path)
	}

	client, err := k8s.GetClusterClient(req.KubeconfigPath, req.Context, logger)
	if err != nil {
		return fmt.Errorf("failed to get cluster client: %w", err)
	}

	ns := req.Namespace
	if ns == "" {
		ns = client.NsInContext
	}

	pvcInfo, err := pvc.New(ctx, client, ns, req.PVCName)
	if err != nil {
		return fmt.Errorf("failed to get PVC info: %w", err)
	}

	if err = handleMounted(pvcInfo, req.IgnoreMounted, logger); err != nil {
		return err
	}

	rcloneCmd := rclone.Cmd{
		Direction:  req.Direction,
		RemotePath: remotePath,
		LocalPath:  localPath,
		ConfigPath: "/etc/rclone/rclone.conf",
		ExtraArgs:  req.RcloneExtraArgs,
		Delete:     req.DeleteExtraneousFiles,
	}

	cmdStr, err := rcloneCmd.Build()
	if err != nil {
		return fmt.Errorf("failed to build rclone command: %w", err)
	}

	helmChart, err := helm.LoadChart(req.ChartVersion)
	if err != nil {
		return fmt.Errorf("failed to load helm chart: %w", err)
	}

	readOnly := req.Direction == rclone.DirectionBackup

	var metadataBase64, metadataRemotePath string

	if shouldUploadMetadata(req) {
		metadataBase64, err = generateMetadataBase64(ns, req.PVCName)
		if err != nil {
			return fmt.Errorf("failed to generate backup metadata: %w", err)
		}

		metadataRemotePath = rclone.BuildMetadataRemotePath(req.Bucket, req.Prefix, req.Name)
	}

	helmVals := buildHelmValues(ns, req, pvcInfo, rcloneConf, cmdStr, readOnly, metadataBase64, metadataRemotePath)

	releaseName := opid.ReleasePrefix + operationID + "-" + req.Direction

	logger = logger.With("release", releaseName)
	logger.Info("📦 Installing Helm chart")

	if err = installHelmChart(helmChart, pvcInfo, releaseName, helmVals, req, logger); err != nil {
		// A timed-out install means resources that are stuck rather than absent,
		// and this path runs no cleanup, so they are still there to be read.
		writeFailure(ctx, req, client.KubeClient, ns, releaseName, err, logger)

		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	jobName := releaseName + "-rclone"

	return handleJobCompletion(ctx, req, pvcInfo, releaseName, jobName, operationID, logger)
}

func buildRcloneConfig(req *Request) (string, error) {
	if req.RcloneConfigFile != "" {
		conf, err := rclone.ReadConfigFile(req.RcloneConfigFile)
		if err != nil {
			return "", fmt.Errorf("failed to read rclone config file: %w", err)
		}

		return conf, nil
	}

	opts := rclone.ConfigOptions{
		Backend:               req.Backend,
		Provider:              req.S3Provider,
		Endpoint:              req.Endpoint,
		Region:                req.Region,
		AccessKey:             req.AccessKey,
		SecretKey:             req.SecretKey,
		StorageAccount:        req.StorageAccount,
		StorageKey:            req.StorageKey,
		GCSServiceAccountJSON: req.GCSServiceAccountJSON,
		GCSBucketPolicyOnly:   req.GCSBucketPolicyOnly,
	}

	conf, err := rclone.GenerateConfig(opts)
	if err != nil {
		return "", fmt.Errorf("failed to generate rclone config: %w", err)
	}

	return conf, nil
}

func buildRemotePath(req *Request) (string, error) {
	if req.RcloneConfigFile != "" {
		if req.Remote == "" {
			return "", errors.New("--remote is required when using --rclone-config")
		}

		return rclone.BuildRemotePathRaw(req.Remote), nil
	}

	if req.Bucket == "" {
		return "", errors.New("--bucket is required")
	}

	if req.Name == "" {
		return "", errors.New("--name is required")
	}

	// The bucket is part of the same object path as the prefix and the name, so it
	// goes through the same rule rather than being the one segment nobody checks.
	if err := validateBucketSegment(req.Bucket, "bucket"); err != nil {
		return "", err
	}

	if err := ValidateName(req.Name); err != nil {
		return "", err
	}

	if err := ValidatePrefix(req.Prefix); err != nil {
		return "", err
	}

	return rclone.BuildRemotePath(req.Bucket, req.Prefix, req.Name), nil
}

func shouldUploadMetadata(req *Request) bool {
	return req.Direction == rclone.DirectionBackup &&
		req.RcloneConfigFile == "" &&
		!hasRcloneDryRun(req.RcloneExtraArgs)
}

// shortFlagCluster matches a run of rclone short flags, which are letters only.
// Requiring the whole token to be letters keeps a value like -1n from reading as
// a dry run.
var shortFlagCluster = regexp.MustCompile(`^-[a-zA-Z]+$`)

// hasRcloneDryRun reports whether the extra rclone arguments ask for a dry run,
// in which case the metadata sidecar must not be uploaded: a run that transfers
// nothing has no business writing a real object to the bucket.
func hasRcloneDryRun(extraArgs string) bool {
	// pflag applies each occurrence in order, so a later one overrides an earlier
	// one and the last is what rclone ends up with. Answering on the first match
	// instead would get "--dry-run=false -n" and "-n --dry-run=false" both wrong,
	// in opposite directions.
	dryRun := false

	for arg := range strings.FieldsSeq(extraArgs) {
		name, value, assigned := strings.Cut(arg, "=")

		if assigned {
			applyAssignedFlag(&dryRun, name, value)

			continue
		}

		// rclone uses pflag, so -n may be bundled with other short flags, as in -nv.
		if arg == "--dry-run" || (shortFlagCluster.MatchString(arg) && strings.Contains(arg, "n")) {
			dryRun = true
		}
	}

	return dryRun
}

// applyAssignedFlag applies one `name=value` argument to the dry-run state.
//
// A boolean flag's value is read with strconv.ParseBool, so "1", "T" and "TRUE"
// ask for a dry run just as much as "true" does, and "0" does not.
func applyAssignedFlag(dryRun *bool, name, value string) {
	switch {
	case name == "--dry-run":
		setFromBool(dryRun, value)
	case shortFlagCluster.MatchString(name):
		// pflag walks a bundle left to right, setting each shorthand on its own, and
		// gives the assigned value only to the last one. So -vn=false turns the dry run
		// off, while -nv=false leaves it on because there the n was already set bare.
		letters := strings.TrimPrefix(name, "-")

		for i, letter := range letters {
			switch {
			case letter != 'n':
			case i == len(letters)-1:
				setFromBool(dryRun, value)
			default:
				*dryRun = true
			}
		}
	}
}

// setFromBool leaves the target alone when the value is not a boolean at all,
// since rclone would reject such an argument itself.
func setFromBool(target *bool, value string) {
	if enabled, err := strconv.ParseBool(value); err == nil {
		*target = enabled
	}
}

func validateBucketSegment(value, flag string) error {
	if safeBucketSegment.MatchString(value) {
		return nil
	}

	return fmt.Errorf("--%s %q contains invalid characters (allowed: alphanumeric, hyphens, underscores, dots)",
		flag, value)
}

// ValidateName validates a managed backup name for use in bucket object paths.
func ValidateName(name string) error {
	return validateBucketSegment(name, "name")
}

// ValidatePrefix validates a managed backup prefix for use in bucket object paths.
func ValidatePrefix(prefix string) error {
	return validatePrefix(prefix)
}

func validatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}

	for segment := range strings.SplitSeq(prefix, "/") {
		if segment == "" {
			return fmt.Errorf(
				"--prefix %q is invalid: must not have leading/trailing '/' or empty path segments",
				prefix,
			)
		}

		if err := validateBucketSegment(segment, "prefix"); err != nil {
			return fmt.Errorf("--prefix %q contains invalid characters (allowed: slash-separated segments of "+
				"alphanumeric, hyphens, underscores, dots)", prefix)
		}
	}

	return nil
}

func buildHelmValues(
	namespace string,
	req *Request,
	pvcInfo *pvc.Info,
	rcloneConf, cmdStr string,
	readOnly bool,
	metadataBase64, metadataRemotePath string,
) map[string]any {
	rcloneVals := map[string]any{
		"enabled":     true,
		"namespace":   namespace,
		"configMount": true,
		"config":      rcloneConf,
		"command":     cmdStr,
		"extraArgs":   "",
		"pvcMounts": []map[string]any{
			{
				"name":      pvcInfo.Claim.Name,
				"mountPath": dataMountPath,
				"readOnly":  readOnly,
			},
		},
		"affinity": pvcInfo.AffinityHelmValues,
	}

	if metadataBase64 != "" {
		rcloneVals["metadataBase64"] = metadataBase64
		rcloneVals["metadataRemotePath"] = metadataRemotePath
	}

	vals := map[string]any{
		"rclone": rcloneVals,
	}

	if req.NonRoot {
		applyNonRootValues(vals)
	}

	return vals
}

func applyNonRootValues(vals map[string]any) {
	rcloneSection, ok := vals["rclone"].(map[string]any)
	if !ok {
		return
	}

	rcloneSection["securityContext"] = map[string]any{
		"runAsNonRoot":             true,
		"runAsUser":                nonRootUID,
		"runAsGroup":               nonRootUID,
		"allowPrivilegeEscalation": false,
	}
	rcloneSection["podSecurityContext"] = map[string]any{
		"fsGroup": nonRootUID,
	}
}

func installHelmChart(
	helmChart *chart.Chart,
	pvcInfo *pvc.Info,
	releaseName string,
	baseValues map[string]any,
	req *Request,
	logger *slog.Logger,
) error {
	actionConfig := new(action.Configuration)

	err := actionConfig.Init(pvcInfo.ClusterClient.RESTClientGetter,
		pvcInfo.Claim.Namespace, os.Getenv("HELM_DRIVER"))
	if err != nil {
		return fmt.Errorf("failed to initialize helm action config: %w", err)
	}

	install := action.NewInstall(actionConfig)
	install.Namespace = pvcInfo.Claim.Namespace
	install.ReleaseName = releaseName
	install.WaitStrategy = kube.LegacyStrategy
	install.Timeout = req.HelmTimeout

	merged, err := mergeHelmValues(baseValues, req, logger)
	if err != nil {
		return err
	}

	if _, err = install.Run(helmChart, merged); err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	return nil
}

func mergeHelmValues(baseValues map[string]any, req *Request, logger *slog.Logger) (map[string]any, error) {
	helmValues := req.HelmValues
	if tag := req.ImageTag; tag != "" {
		helmValues = append([]string{"rclone.image.tag=" + tag}, req.HelmValues...)
	}

	valsOptions := values.Options{
		ValueFiles:   req.HelmValuesFiles,
		Values:       helmValues,
		StringValues: req.HelmStringValues,
		FileValues:   req.HelmFileValues,
	}

	userValues, err := valsOptions.MergeValues(helmProviders)
	if err != nil {
		return nil, fmt.Errorf("failed to merge helm values: %w", err)
	}

	merged := loader.MergeMaps(baseValues, userValues)

	if req.ImageTag != "" {
		logger.Info("🏷️ Using image tag", "tag", req.ImageTag)
	} else {
		logger.Info("🏷️ Using chart default image tags")
	}

	return merged, nil
}

func handleJobCompletion(
	ctx context.Context,
	req *Request,
	pvcInfo *pvc.Info,
	releaseName, jobName, operationID string,
	logger *slog.Logger,
) (retErr error) {
	kubeClient := pvcInfo.ClusterClient.KubeClient
	namespace := pvcInfo.Claim.Namespace

	defer func() {
		if req.NoCleanup {
			logger.Info("🧹 Cleanup skipped")

			return
		}

		if req.NoCleanupOnFailure && retErr != nil {
			logger.Info("🧹 Cleanup skipped (operation failed, resources left for inspection)")

			return
		}

		if req.Detach {
			return
		}

		if cleanupErr := cleanupRelease(pvcInfo, releaseName, req.HelmTimeout); cleanupErr != nil {
			logger.Warn("🔶 Cleanup failed, you might want to clean up manually", "error", cleanupErr)
		} else {
			logger.Info("✨ Cleanup done")
		}
	}()

	if req.Detach {
		if _, err := k8s.WaitForJobStart(ctx, kubeClient, namespace, jobName, logger); err != nil {
			return fmt.Errorf("failed to wait for job to start: %w", err)
		}

		printDetachMessage(req, operationID, logger)

		return nil
	}

	if err := k8s.WaitForJobCompletion(ctx, kubeClient, namespace, jobName,
		shouldShowProgressBar(req.Writer), req.StructuredLogs,
		console.Palette{Enabled: req.ColorOutput}, req.Writer, logger); err != nil {
		// Before the deferred cleanup removes the resources this is about.
		writeFailure(ctx, req, kubeClient, namespace, releaseName, err, logger)

		// Deliberately unwrapped: the error already names the failed pod and the
		// exit state, the public API adds the operation prefix, and a plumbing
		// wrap in between would push the answer further right.
		return err //nolint:wrapcheck
	}

	logger.Info("✅ Operation succeeded")

	return nil
}

// writeFailure explains a failure the way the migration summary does: a heading,
// the cause on an indented line beneath it, then what the cluster reported. On a
// structured log stream the plain-text block would corrupt the records around
// it, so it travels inside a record instead.
func writeFailure(
	ctx context.Context,
	req *Request,
	kubeClient kubernetes.Interface,
	namespace, releaseName string,
	cause error,
	logger *slog.Logger,
) {
	if req.StructuredLogs {
		var buf bytes.Buffer

		k8s.WriteWorkloadDiagnostics(ctx, kubeClient, namespace,
			k8s.InstanceLabelSelector(releaseName), console.Palette{}, &buf, logger)

		logger.Error("❌ What the cluster reported", "release", releaseName,
			"namespace", namespace, "diagnostics", buf.String())

		return
	}

	palette := console.Palette{Enabled: req.ColorOutput}

	fmt.Fprintf(req.Writer, "%s\n", palette.Failure(capitalizedDirection(req.Direction)+" failed."))

	if cause != nil {
		for line := range strings.SplitSeq(cause.Error(), "\n") {
			fmt.Fprintf(req.Writer, "    %s\n", line)
		}
	}

	fmt.Fprintf(req.Writer, "\n%s\n\n  %s (namespace %s):\n",
		palette.Bold("What the cluster reported:"), releaseName, namespace)
	k8s.WriteWorkloadDiagnostics(ctx, kubeClient, namespace,
		k8s.InstanceLabelSelector(releaseName), palette, req.Writer, logger)
	fmt.Fprintln(req.Writer)
}

func capitalizedDirection(direction string) string {
	if direction == "" {
		return "Operation"
	}

	return strings.ToUpper(direction[:1]) + direction[1:]
}

func cleanupRelease(pvcInfo *pvc.Info, releaseName string, timeout time.Duration) error {
	actionConfig := new(action.Configuration)

	err := actionConfig.Init(pvcInfo.ClusterClient.RESTClientGetter,
		pvcInfo.Claim.Namespace, os.Getenv("HELM_DRIVER"))
	if err != nil {
		return fmt.Errorf("failed to initialize helm action config: %w", err)
	}

	uninstall := action.NewUninstall(actionConfig)
	uninstall.WaitStrategy = kube.LegacyStrategy
	uninstall.Timeout = timeout

	if _, err = uninstall.Run(releaseName); err != nil {
		return fmt.Errorf("failed to uninstall helm release %s: %w", releaseName, err)
	}

	return nil
}

func handleMounted(info *pvc.Info, ignoreMounted bool, logger *slog.Logger) error {
	if info.MountedNode == "" {
		return nil
	}

	if ignoreMounted {
		logger.Info("💡 PVC is mounted to a node, but --ignore-mounted is requested, ignoring...",
			"pvc", info.Claim.Namespace+"/"+info.Claim.Name, "mounted_node", info.MountedNode)

		return nil
	}

	return fmt.Errorf("PVC is mounted to a node and --ignore-mounted is not requested: "+
		"node: %s, claim: %s/%s", info.MountedNode, info.Claim.Namespace, info.Claim.Name)
}

// validateSubpath ensures the path is a relative subpath that stays under the mount root.
func validateSubpath(p string) error {
	if path.IsAbs(p) {
		return errors.New("must be a relative path")
	}

	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("must not escape the volume root with '..'")
	}

	return nil
}

func printDetachMessage(req *Request, operationID string, logger *slog.Logger) {
	logger.Info("🚀 Operation detached", "id", operationID, "direction", req.Direction)

	fmt.Fprintln(req.Writer)
	fmt.Fprintf(req.Writer, "%s %s detached. The rclone job is running in the cluster.\n",
		req.Direction, operationID)
	fmt.Fprintln(req.Writer)
	fmt.Fprintln(req.Writer, "To check status:")
	fmt.Fprintf(req.Writer, "  pv-migrate status %s\n", operationID)
	fmt.Fprintln(req.Writer)
	fmt.Fprintln(req.Writer, "To clean up after completion:")
	fmt.Fprintf(req.Writer, "  pv-migrate cleanup %s\n", operationID)
	fmt.Fprintln(req.Writer)
}

func shouldShowProgressBar(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}

	return isatty.IsTerminal(file.Fd())
}
