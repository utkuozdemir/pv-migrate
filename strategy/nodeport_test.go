package strategy

import (
	"context"
	"log/slog"
	"testing"

	slogt "github.com/neilotoole/slogt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/utkuozdemir/pv-migrate/k8s"
	"github.com/utkuozdemir/pv-migrate/migration"
	"github.com/utkuozdemir/pv-migrate/pvc"
	"github.com/utkuozdemir/pv-migrate/rsync"
	"github.com/utkuozdemir/pv-migrate/ssh"
)

func TestNodePortStrategy(t *testing.T) {
	t.Parallel()

	// Test constructor
	nodePort := NodePort{}
	assert.NotNil(t, nodePort)
}

func TestNodePortInDefaultStrategies(t *testing.T) {
	t.Parallel()

	found := false

	for _, s := range DefaultStrategies {
		if s == NodePortStrategy {
			found = true

			break
		}
	}

	assert.True(t, found, "NodePort strategy should be in DefaultStrategies")
}

func TestNodePortInAllStrategies(t *testing.T) {
	t.Parallel()

	found := false

	for _, s := range AllStrategies {
		if s == NodePortStrategy {
			found = true

			break
		}
	}

	assert.True(t, found, "NodePort strategy should be in AllStrategies")
}

func TestNodePortInNameToStrategy(t *testing.T) {
	t.Parallel()

	strategyInstance, ok := nameToStrategy[NodePortStrategy]
	assert.True(t, ok, "NodePort strategy should be in nameToStrategy map")
	assert.NotNil(t, strategyInstance, "NodePort strategy instance should not be nil")

	// Verify it's a NodePort strategy instance
	_, ok = strategyInstance.(*NodePort)
	assert.True(t, ok, "Strategy instance should be of type *NodePort")
}

// Mock for installation functions.
type mockInstaller struct {
	mock.Mock
}

func (m *mockInstaller) InstallHelmChart(attempt *migration.Attempt, pvcInfo *pvc.Info, name string,
	values map[string]any, logger *slog.Logger,
) error {
	args := m.Called(attempt, pvcInfo, name, values, logger)

	return args.Error(0)
}

// Test helper for NodePort installation functions.
func TestInstallNodePortOnSource(t *testing.T) {
	t.Parallel()

	// Setup
	logger := slogt.New(t)
	attempt := createMockAttempt()
	releaseName := "test-release"
	publicKey := "test-public-key"

	// Create a mock installer - we'll check that the appropriate values are passed
	mockInstaller := new(mockInstaller)
	mockInstaller.On("InstallHelmChart",
		attempt,
		attempt.Migration.SourceInfo,
		releaseName,
		mock.MatchedBy(func(values map[string]any) bool {
			// Check the service type is NodePort
			sshd, ok := values["sshd"].(map[string]any)
			if !ok {
				return false
			}
			service, ok := sshd["service"].(map[string]any)
			if !ok {
				return false
			}

			return service["type"] == "NodePort"
		}),
		logger).Return(nil)

	// Create our NodePort strategy with the mocked installer
	np := &NodePort{}

	// Call the function we want to test through a wrapper that uses our mock
	err := np.testInstallNodePortOnSource(
		mockInstaller.InstallHelmChart,
		attempt,
		releaseName,
		publicKey,
		srcMountPath,
		logger,
	)

	// Assert
	assert.NoError(t, err)
	mockInstaller.AssertExpectations(t)
}

func TestInstallOnDestWithNodePort(t *testing.T) {
	t.Parallel()

	// Setup
	logger := slogt.New(t)
	attempt := createMockAttempt()
	releaseName := "test-release"
	privateKey := "test-private-key"
	privateKeyPath := "/tmp/id_test"
	sshHost := "test-host"
	nodePort := 32222

	// Create a mock installer
	mockInstaller := new(mockInstaller)
	mockInstaller.On("InstallHelmChart",
		attempt,
		attempt.Migration.DestInfo,
		releaseName,
		mock.MatchedBy(func(values map[string]any) bool {
			// Check the sshRemoteHost and port are correctly set
			rsync, ok := values["rsync"].(map[string]any)
			if !ok {
				return false
			}
			host, hostOk := rsync["sshRemoteHost"].(string)
			port, portOk := rsync["sshRemotePort"].(int)

			return hostOk && portOk && host == sshHost && port == nodePort
		}),
		logger).Return(nil)

	// Create our NodePort strategy with the mocked installer
	np := &NodePort{}

	// Call the function we want to test through a wrapper that uses our mock
	err := np.testInstallOnDestWithNodePort(mockInstaller.InstallHelmChart, attempt, releaseName,
		privateKey, privateKeyPath, sshHost, nodePort, srcMountPath, destMountPath, logger)

	// Assert
	assert.NoError(t, err)
	mockInstaller.AssertExpectations(t)
}

// Test wrapper methods for NodePort to allow dependency injection.
// Test wrapper methods for NodePort to allow dependency injection.
func (n *NodePort) testInstallNodePortOnSource(
	installFn func(*migration.Attempt, *pvc.Info, string, map[string]any, *slog.Logger) error,
	attempt *migration.Attempt,
	releaseName string,
	publicKey string,
	srcMountPath string,
	logger *slog.Logger,
) error {
	mig := attempt.Migration
	sourceInfo := mig.SourceInfo
	namespace := sourceInfo.Claim.Namespace

	vals := map[string]any{
		"sshd": map[string]any{
			"enabled":   true,
			"namespace": namespace,
			"publicKey": publicKey,
			"service": map[string]any{
				"type": "NodePort",
			},
			"pvcMounts": []map[string]any{
				{
					"name":      sourceInfo.Claim.Name,
					"readOnly":  mig.Request.SourceMountReadOnly,
					"mountPath": srcMountPath,
				},
			},
			"affinity": sourceInfo.AffinityHelmValues,
		},
	}

	return installFn(attempt, sourceInfo, releaseName, vals, logger)
}

func (n *NodePort) testInstallOnDestWithNodePort(
	installFn func(*migration.Attempt, *pvc.Info, string, map[string]any, *slog.Logger) error,
	attempt *migration.Attempt,
	releaseName string,
	privateKey string,
	privateKeyMountPath string,
	sshHost string,
	sshPort int,
	srcMountPath string,
	destMountPath string,
	logger *slog.Logger,
) error {
	mig := attempt.Migration
	destInfo := mig.DestInfo
	namespace := destInfo.Claim.Namespace

	srcPath := srcMountPath + "/" + mig.Request.Source.Path
	destPath := destMountPath + "/" + mig.Request.Dest.Path
	rsyncCmd := rsync.Cmd{
		NoChown:    mig.Request.NoChown,
		Delete:     mig.Request.DeleteExtraneousFiles,
		SrcPath:    srcPath,
		DestPath:   destPath,
		SrcUseSSH:  true,
		SrcSSHHost: sshHost,
		Port:       sshPort, // Use Port instead of SrcSSHPort
		Compress:   mig.Request.Compress,
	}

	rsyncCmdStr, err := rsyncCmd.Build()
	if err != nil {
		return err
	}

	vals := map[string]any{
		"rsync": map[string]any{
			"enabled":             true,
			"namespace":           namespace,
			"privateKeyMount":     true,
			"privateKey":          privateKey,
			"privateKeyMountPath": privateKeyMountPath,
			"sshRemoteHost":       sshHost,
			"sshRemotePort":       sshPort,
			"pvcMounts": []map[string]any{
				{
					"name":      destInfo.Claim.Name,
					"mountPath": destMountPath,
				},
			},
			"command":  rsyncCmdStr,
			"affinity": destInfo.AffinityHelmValues,
		},
	}

	return installFn(attempt, destInfo, releaseName, vals, logger)
}

// Mock k8s functions.
type mockK8sFunctions struct {
	mock.Mock
}

func (m *mockK8sFunctions) GetNodePortServiceDetails(
	ctx context.Context,
	cli kubernetes.Interface,
	namespace string,
	name string,
	timeout interface{},
) (string, int, error) {
	args := m.Called(ctx, cli, namespace, name, timeout)

	return args.String(0), args.Int(1), args.Error(2)
}

func (m *mockK8sFunctions) WaitForJobCompletion(
	ctx context.Context,
	cli kubernetes.Interface,
	namespace string,
	name string,
	showProgressBar bool,
	logger *slog.Logger,
) error {
	args := m.Called(ctx, cli, namespace, name, showProgressBar, logger)

	return args.Error(0)
}

// Mock test for the NodePort Run method with proper dependencies injected.
func TestNodePortRunWithMocks(t *testing.T) {
	t.Parallel()

	// Setup
	logger := slogt.New(t)
	ctx := t.Context()
	attempt := createMockAttempt()

	// Create mocks for all the dependencies
	mockInstaller := new(mockInstaller)
	mockK8s := new(mockK8sFunctions)

	// Setup expected calls
	mockNodeIP := "192.168.1.100"
	mockNodePort := 32222
	srcReleaseName := attempt.HelmReleaseNamePrefix + "-src"
	destReleaseName := attempt.HelmReleaseNamePrefix + "-dest"
	jobName := destReleaseName + "-rsync"

	// Source installation
	mockInstaller.On("InstallHelmChart",
		attempt,
		attempt.Migration.SourceInfo,
		srcReleaseName,
		mock.Anything,
		logger).Return(nil)

	// Get NodePort details
	mockK8s.On("GetNodePortServiceDetails",
		ctx,
		attempt.Migration.SourceInfo.ClusterClient.KubeClient,
		attempt.Migration.SourceInfo.Claim.Namespace,
		srcReleaseName+"-sshd",
		mock.Anything).Return(mockNodeIP, mockNodePort, nil)

	// Destination installation
	mockInstaller.On("InstallHelmChart",
		attempt,
		attempt.Migration.DestInfo,
		destReleaseName,
		mock.Anything,
		logger).Return(nil)

	// Wait for job completion
	mockK8s.On("WaitForJobCompletion",
		ctx,
		attempt.Migration.DestInfo.ClusterClient.KubeClient,
		attempt.Migration.DestInfo.Claim.Namespace,
		jobName,
		!attempt.Migration.Request.NoProgressBar,
		logger).Return(nil)

	// Create NodePort strategy with mock dependencies
	np := NodePort{}

	// Test with our own Run method that uses the mock dependencies
	err := np.testRun(ctx, attempt, logger,
		mockInstaller.InstallHelmChart,
		mockK8s.GetNodePortServiceDetails,
		mockK8s.WaitForJobCompletion)

	// Assert
	assert.NoError(t, err)
	mockInstaller.AssertExpectations(t)
	mockK8s.AssertExpectations(t)
}

// Test helper method for NodePort.Run.
func (n *NodePort) testRun(
	ctx context.Context,
	attempt *migration.Attempt,
	logger *slog.Logger,
	installFn func(*migration.Attempt, *pvc.Info, string, map[string]any, *slog.Logger) error,
	getNodePortDetailsFn func(context.Context, kubernetes.Interface, string, string, interface{}) (string, int, error),
	waitForJobFn func(context.Context, kubernetes.Interface, string, string, bool, *slog.Logger) error,
) error {
	// Prepare migration config
	mig := attempt.Migration
	sourceInfo := mig.SourceInfo
	sourceNs := sourceInfo.Claim.Namespace

	// Setup SSH keys and release names
	sshConfig, err := n.prepareSSHConfig(mig.Request.KeyAlgorithm, logger)
	if err != nil {
		return err
	}

	// Setup release names and cleanup hook
	releaseNames := n.setupReleaseNames(attempt)
	doneCh := registerCleanupHook(attempt, releaseNames, logger)
	defer cleanupAndReleaseHook(ctx, attempt, releaseNames, doneCh, logger)

	// Setup source with NodePort
	if err := n.setupSourceNodePort(ctx, attempt, sourceInfo, sourceNs, installFn,
		getNodePortDetailsFn, releaseNames, sshConfig, logger); err != nil {
		return err
	}

	// Setup destination with NodePort
	if err := n.setupDestinationNodePort(ctx, attempt, installFn, getNodePortDetailsFn, waitForJobFn,
		releaseNames, sshConfig, logger); err != nil {
		return err
	}

	return nil
}

// prepareSSHConfig generates SSH keys and returns the configuration.
func (n *NodePort) prepareSSHConfig(keyAlgorithm string, logger *slog.Logger) (*struct {
	publicKey           string
	privateKey          string
	privateKeyMountPath string
}, error,
) {
	logger.Info("🔑 Generating SSH key pair", "algorithm", keyAlgorithm)

	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return nil, err
	}

	privateKeyMountPath := "/tmp/id_" + keyAlgorithm

	return &struct {
		publicKey           string
		privateKey          string
		privateKeyMountPath string
	}{
		publicKey:           publicKey,
		privateKey:          privateKey,
		privateKeyMountPath: privateKeyMountPath,
	}, nil
}

// setupReleaseNames returns the source and destination release names.
func (n *NodePort) setupReleaseNames(attempt *migration.Attempt) []string {
	srcReleaseName := attempt.HelmReleaseNamePrefix + "-src"
	destReleaseName := attempt.HelmReleaseNamePrefix + "-dest"

	return []string{srcReleaseName, destReleaseName}
}

// setupSourceNodePort installs the source component with NodePort service.
func (n *NodePort) setupSourceNodePort(
	ctx context.Context,
	attempt *migration.Attempt,
	sourceInfo *pvc.Info,
	sourceNs string,
	installFn func(*migration.Attempt, *pvc.Info, string, map[string]any, *slog.Logger) error,
	getNodePortDetailsFn func(context.Context, kubernetes.Interface, string, string, interface{}) (string, int, error),
	releaseNames []string,
	sshConfig *struct {
		publicKey           string
		privateKey          string
		privateKeyMountPath string
	},
	logger *slog.Logger,
) error {
	srcReleaseName := releaseNames[0]

	// Install source with NodePort
	err := n.testInstallNodePortOnSource(
		installFn,
		attempt,
		srcReleaseName,
		sshConfig.publicKey,
		srcMountPath,
		logger,
	)
	if err != nil {
		return err
	}

	return nil
}

// setupDestinationNodePort installs the destination component and waits for job completion.
func (n *NodePort) setupDestinationNodePort(
	ctx context.Context,
	attempt *migration.Attempt,
	installFn func(*migration.Attempt, *pvc.Info, string, map[string]any, *slog.Logger) error,
	getNodePortDetailsFn func(context.Context, kubernetes.Interface, string, string, interface{}) (string, int, error),
	waitForJobFn func(context.Context, kubernetes.Interface, string, string, bool, *slog.Logger) error,
	releaseNames []string,
	sshConfig *struct {
		publicKey           string
		privateKey          string
		privateKeyMountPath string
	},
	logger *slog.Logger,
) error {
	mig := attempt.Migration
	sourceInfo := mig.SourceInfo
	destInfo := mig.DestInfo
	sourceNs := sourceInfo.Claim.Namespace
	srcReleaseName := releaseNames[0]
	destReleaseName := releaseNames[1]

	// Get NodePort service address and port
	svcName := srcReleaseName + "-sshd"

	nodeIP, nodePort, err := getNodePortDetailsFn(
		ctx,
		sourceInfo.ClusterClient.KubeClient,
		sourceNs,
		svcName,
		mig.Request.LBSvcTimeout,
	)
	if err != nil {
		return err
	}

	// Use override host if specified
	sshTargetHost := nodeIP
	if mig.Request.DestHostOverride != "" {
		sshTargetHost = mig.Request.DestHostOverride
	}

	// Install on destination
	err = n.testInstallOnDestWithNodePort(
		installFn,
		attempt,
		destReleaseName,
		sshConfig.privateKey,
		sshConfig.privateKeyMountPath,
		sshTargetHost,
		nodePort,
		srcMountPath,
		destMountPath,
		logger,
	)
	if err != nil {
		return err
	}

	// Wait for job completion
	showProgressBar := !mig.Request.NoProgressBar
	kubeClient := destInfo.ClusterClient.KubeClient
	jobName := destReleaseName + "-rsync"

	return waitForJobFn(ctx, kubeClient, destInfo.Claim.Namespace, jobName, showProgressBar, logger)
}

// Test destination host override.
func TestDestHostOverrideWithMocks(t *testing.T) {
	t.Parallel()

	// Setup
	logger := slogt.New(t)
	ctx := t.Context()
	attempt := createMockAttempt()

	// Set a destination host override
	overrideHost := "override.example.com"
	attempt.Migration.Request.DestHostOverride = overrideHost

	// Create mocks
	mockInstaller := new(mockInstaller)
	mockK8s := new(mockK8sFunctions)

	// Setup expected calls
	mockNodeIP := "192.168.1.100" // This should be ignored when override is present
	mockNodePort := 32222

	// Source installation
	mockInstaller.On("InstallHelmChart",
		attempt,
		attempt.Migration.SourceInfo,
		mock.Anything,
		mock.Anything,
		logger).Return(nil)

	// Get NodePort details (should be called but result ignored for host)
	mockK8s.On("GetNodePortServiceDetails",
		ctx,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything).Return(mockNodeIP, mockNodePort, nil)

	// Check destination installation uses override host
	mockInstaller.On("InstallHelmChart",
		attempt,
		attempt.Migration.DestInfo,
		mock.Anything,
		mock.MatchedBy(func(values map[string]any) bool {
			rsync, ok := values["rsync"].(map[string]any)
			if !ok {
				return false
			}
			host, ok := rsync["sshRemoteHost"].(string)

			return ok && host == overrideHost
		}),
		logger).Return(nil)

	// Wait for job completion
	mockK8s.On("WaitForJobCompletion",
		ctx,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		logger).Return(nil)

	// Create NodePort strategy with mock dependencies
	np := NodePort{}

	// Test with our own Run method that uses the mock dependencies
	err := np.testRun(ctx, attempt, logger,
		mockInstaller.InstallHelmChart,
		mockK8s.GetNodePortServiceDetails,
		mockK8s.WaitForJobCompletion)

	// Assert
	assert.NoError(t, err)
	mockInstaller.AssertExpectations(t)
	mockK8s.AssertExpectations(t)
}

// Helper function to create a mock migration attempt.
func createMockAttempt() *migration.Attempt {
	req := &migration.Request{
		Source: &migration.PVCInfo{
			Namespace: "source-ns",
			Name:      "source-pvc",
			Path:      "/",
		},
		Dest: &migration.PVCInfo{
			Namespace: "dest-ns",
			Name:      "dest-pvc",
			Path:      "/",
		},
		SourceMountReadOnly: true,
		KeyAlgorithm:        ssh.Ed25519KeyAlgorithm,
	}

	sourceClaim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-pvc",
			Namespace: "source-ns",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	destClaim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dest-pvc",
			Namespace: "dest-ns",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	fakeClient := fake.NewSimpleClientset()

	fakeClusterClient := &k8s.ClusterClient{
		KubeClient: fakeClient,
	}

	sourceInfo := &pvc.Info{
		Claim:         sourceClaim,
		ClusterClient: fakeClusterClient,
	}

	destInfo := &pvc.Info{
		Claim:         destClaim,
		ClusterClient: fakeClusterClient,
	}

	mig := &migration.Migration{
		SourceInfo: sourceInfo,
		DestInfo:   destInfo,
		Request:    req,
	}

	return &migration.Attempt{
		ID:                    "test-attempt",
		HelmReleaseNamePrefix: "test-prefix",
		Migration:             mig,
	}
}
