package bucketstorage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
	"github.com/mattn/go-isatty"
	"helm.sh/helm/v4/pkg/action"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/cli/values"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/kube"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/rclone"
)

const (
	dataMountPath = "/data"
	nonRootUID    = 10000
)

// safeBucketSegment matches strings that are safe for use in bucket paths:
// alphanumeric, hyphens, underscores, and dots.
var safeBucketSegment = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

var helmProviders = getter.All(cli.New())

// Request holds all parameters for a backup or restore operation.
type Request struct {
	ID                 string
	ImageTag           string
	ChartVersion       string
	Direction          string // rclone.DirectionBackup or rclone.DirectionRestore
	KubeconfigPath     string
	Context            string
	Namespace          string
	PVCName            string
	IgnoreMounted      bool
	NonRoot            bool
	Detach             bool
	NoCleanup          bool
	NoCleanupOnFailure bool

	// Bucket storage config
	Backend               string
	Bucket                string
	Endpoint              string
	Region                string
	AccessKey             string
	SecretKey             string
	StorageAccount        string
	StorageKey            string
	GCSServiceAccountJSON string
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
}

// Run executes a backup or restore operation.
//
//nolint:cyclop,funlen
func Run(ctx context.Context, req *Request) error {
	logger := req.Logger

	migrationID := req.ID
	if migrationID == "" {
		migrationID = petname.Generate(2, "-")
	}

	logger = logger.With("id", migrationID, "direction", req.Direction)

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

	if req.Direction == rclone.DirectionBackup && req.RcloneConfigFile == "" {
		metadataBase64, err = generateMetadataBase64(ns, req.PVCName)
		if err != nil {
			return fmt.Errorf("failed to generate backup metadata: %w", err)
		}

		metadataRemotePath = rclone.BuildMetadataRemotePath(req.Bucket, req.Prefix, req.Name)
	}

	helmVals := buildHelmValues(ns, req, pvcInfo, rcloneConf, cmdStr, readOnly, metadataBase64, metadataRemotePath)

	releaseName := fmt.Sprintf("pv-migrate-%s-%s", migrationID, req.Direction)

	logger = logger.With("release", releaseName)
	logger.Info("📦 Installing Helm chart")

	if err = installHelmChart(helmChart, pvcInfo, releaseName, helmVals, req, logger); err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	jobName := releaseName + "-rclone"

	return handleJobCompletion(ctx, req, pvcInfo, releaseName, jobName, migrationID, logger)
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
		Endpoint:              req.Endpoint,
		Region:                req.Region,
		AccessKey:             req.AccessKey,
		SecretKey:             req.SecretKey,
		StorageAccount:        req.StorageAccount,
		StorageKey:            req.StorageKey,
		GCSServiceAccountJSON: req.GCSServiceAccountJSON,
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

	if err := validateBucketSegment(req.Name, "name"); err != nil {
		return "", err
	}

	if err := validatePrefix(req.Prefix); err != nil {
		return "", err
	}

	return rclone.BuildRemotePath(req.Bucket, req.Prefix, req.Name), nil
}

func validateBucketSegment(value, flag string) error {
	if safeBucketSegment.MatchString(value) {
		return nil
	}

	return fmt.Errorf("--%s %q contains invalid characters (allowed: alphanumeric, hyphens, underscores, dots)",
		flag, value)
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
		imageTagValues := []string{"rclone.image.tag=" + tag}
		merged := make([]string, 0, len(imageTagValues)+len(helmValues))
		merged = append(merged, imageTagValues...)
		helmValues = append(merged, helmValues...)
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
	releaseName, jobName, migrationID string,
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

		printDetachMessage(req, migrationID, logger)

		return nil
	}

	if err := k8s.WaitForJobCompletion(ctx, kubeClient, namespace, jobName,
		shouldShowProgressBar(req.Writer), req.Writer, logger); err != nil {
		return fmt.Errorf("%s failed: %w", req.Direction, err)
	}

	logger.Info("✅ Operation succeeded")

	return nil
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
		"node: %s claim %s", info.MountedNode, info.Claim.Name)
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

func printDetachMessage(req *Request, migrationID string, logger *slog.Logger) {
	logger.Info("🚀 Operation detached", "id", migrationID, "direction", req.Direction)

	fmt.Fprintln(req.Writer)
	fmt.Fprintf(req.Writer, "%s %s detached. The rclone job is running in the cluster.\n",
		req.Direction, migrationID)
	fmt.Fprintln(req.Writer)
	fmt.Fprintln(req.Writer, "To check status:")
	fmt.Fprintf(req.Writer, "  pv-migrate status %s\n", migrationID)
	fmt.Fprintln(req.Writer)
	fmt.Fprintln(req.Writer, "To clean up after completion:")
	fmt.Fprintf(req.Writer, "  pv-migrate cleanup %s\n", migrationID)
	fmt.Fprintln(req.Writer)
}

func shouldShowProgressBar(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}

	return isatty.IsTerminal(file.Fd())
}
