//go:build integration

//nolint:paralleltest
package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/neilotoole/slogt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/remotecommand"
	watchtools "k8s.io/client-go/tools/watch"
	"k8s.io/utils/env"

	"github.com/utkuozdemir/pv-migrate/app"
	"github.com/utkuozdemir/pv-migrate/k8s"
	"github.com/utkuozdemir/pv-migrate/util"
)

const (
	dataFileUID         = "12345"
	dataFileGID         = "54321"
	dataFilePath        = "/volume/file.txt"
	extraDataFilePath   = "/volume/extra_file.txt"
	generateDataContent = "DATA"

	longSourcePvcName = "source-source-source-source-source-source-source-source-source-source-source-source-" +
		"source-source-source-source-source-source-source-source-source-source-source-source-source-source-source"
	longDestPvcName = "dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-" +
		"dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest"

	migrateCmdline       = "--helm-set rsync.networkPolicy.enabled=true --helm-set sshd.networkPolicy.enabled=true"
	migrateLegacyCmdline = "migrate " + migrateCmdline
)

var (
	ns1 string
	ns2 string
	ns3 string

	extraClusterKubeconfig string

	mainClusterCli  *k8s.ClusterClient
	extraClusterCli *k8s.ClusterClient

	generateDataShellCommand = fmt.Sprintf("echo -n %s > %s && chown %s:%s %s",
		generateDataContent, dataFilePath, dataFileUID, dataFileGID, dataFilePath)
	generateExtraDataShellCommand = fmt.Sprintf("echo -n %s > %s",
		generateDataContent, extraDataFilePath)
	printDataUIDGIDContentShellCommand = fmt.Sprintf("stat -c '%%u' %s && stat -c '%%g' %s && cat %s",
		dataFilePath, dataFilePath, dataFilePath)
	checkExtraDataShellCommand = "ls " + extraDataFilePath
	clearDataShellCommand      = "find /volume -mindepth 1 -delete"

	resourceLabels = map[string]string{
		"pv-migrate-test": "true",
	}

	ErrPodExecStderr = errors.New("pod exec stderr")
)

func TestIntegration(t *testing.T) {
	logger := slogt.New(t)

	setup(t, logger)
	teardownOnCleanup(t, logger)

	t.Run("SameNS", testSameNS)
	t.Run("CustomRsyncArgs", testCustomRsyncArgs)
	t.Run("SameNSLbSvc", testSameNSLbSvc)
	t.Run("NoChown", testNoChown)
	t.Run("DeleteExtraneousFiles", testDeleteExtraneousFiles)
	t.Run("MountedError", testMountedError)
	t.Run("DifferentNS", testDifferentNS)
	t.Run("FailWithoutNetworkPolicies", testFailWithoutNetworkPolicies)
	t.Run("LbSvcDestHostOverride", testLbSvcDestHostOverride)
	t.Run("RSA", testRSA)
	t.Run("DifferentCluster", testDifferentCluster)
	t.Run("Local", testLocal)
	t.Run("LongPVCNames", testLongPVCNames)
	t.Run("NodePort", testNodePort)
	t.Run("NodePortDifferentNS", testNodePortDifferentNS)
	t.Run("NodePortDestHostOverride", testNodePortDestHostOverride)
}

// testNodePort tests the NodePort strategy in the same namespace
func testNodePort(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	// Prepare the destination with an extra file to test it remains after migration
	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	// Run the migration using the NodePort strategy specifically
	cmd := fmt.Sprintf("%s -s nodeport -i -n %s -N %s source dest", migrateLegacyCmdline, ns1, ns1)
	require.NoError(t, runCliApp(ctx, cmd))

	// Verify the data was migrated correctly
	stdout, err := execInPod(ctx, mainClusterCli, ns1, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	// Check that ownership and content were preserved
	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	// Verify that the extra file still exists (no deletion)
	_, err = execInPod(ctx, mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

// testNodePortDifferentNS tests the NodePort strategy with source and destination in different namespaces
func testNodePortDifferentNS(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	// Prepare the destination with an extra file to test it remains after migration
	_, err := execInPod(ctx, mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	// Run the migration using the NodePort strategy specifically between different namespaces
	cmd := fmt.Sprintf("%s -s nodeport -i -n %s -N %s --source source --dest dest",
		migrateCmdline, ns1, ns2)
	require.NoError(t, runCliApp(ctx, cmd))

	// Verify the data was migrated correctly
	stdout, err := execInPod(ctx, mainClusterCli, ns2, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	// Check that ownership and content were preserved
	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	// Verify that the extra file still exists (no deletion)
	_, err = execInPod(ctx, mainClusterCli, ns2, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

// testNodePortDestHostOverride tests the NodePort strategy with a custom destination host override
func testNodePortDestHostOverride(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	// Create a service that will be used for the override
	svcName := "nodeport-override-svc"
	_, err := mainClusterCli.KubeClient.CoreV1().Services(ns1).Create(context.Background(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:   svcName,
				Labels: resourceLabels,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app.kubernetes.io/component": "sshd",
					"app.kubernetes.io/name":      "pv-migrate",
				},
				Ports: []corev1.ServicePort{
					{
						Name:       "ssh",
						Port:       22,
						TargetPort: intstr.FromInt32(22),
					},
				},
			},
		}, metav1.CreateOptions{})
	require.NoError(t, err)

	// Prepare the destination with an extra file to test it remains after migration
	_, err = execInPod(ctx, mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	// Set the destination host override to use our custom service
	destHostOverride := svcName + "." + ns1
	cmd := fmt.Sprintf(
		"%s -s nodeport -i -n %s -N %s -H %s source dest", migrateLegacyCmdline, ns1, ns2, destHostOverride)
	require.NoError(t, runCliApp(ctx, cmd))

	// Verify the data was migrated correctly
	stdout, err := execInPod(ctx, mainClusterCli, ns2, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	// Check that ownership and content were preserved
	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	// Verify that the extra file still exists (no deletion)
	_, err = execInPod(ctx, mainClusterCli, ns2, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

func testSameNS(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -i -n %s -N %s source dest", migrateLegacyCmdline, ns1, ns1)
	require.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns1, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

// TestCustomRsyncArgs is the same as TestSameNS except it also passes custom args to rsync.
func testCustomRsyncArgs(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmdArgs := strings.Fields(fmt.Sprintf("%s -i -n %s -N %s", migrateLegacyCmdline, ns1, ns1))
	cmdArgs = append(cmdArgs, "--helm-set", "rsync.extraArgs=--partial --inplace --sparse", "source", "dest")

	require.NoError(t, runCliAppWithArgs(ctx, cmdArgs...))

	stdout, err := execInPod(ctx, mainClusterCli, ns1, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

func testSameNSLbSvc(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -s lbsvc -i -n %s -N %s --lbsvc-timeout 5m source dest", migrateLegacyCmdline, ns1, ns1)
	require.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns1, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

func testNoChown(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -i -o -n %s -N %s source dest", migrateLegacyCmdline, ns1, ns1)
	require.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns1, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, "0", parts[0])
	assert.Equal(t, "0", parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

func testDeleteExtraneousFiles(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s --compress=false -d -i -n %s -N %s source dest", migrateLegacyCmdline, ns1, ns1)
	require.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns1, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No such file or directory")
}

func testMountedError(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -n %s -N %s source dest", migrateLegacyCmdline, ns1, ns1)
	err = runCliApp(ctx, cmd)
	assert.ErrorContains(t, err, "ignore-mounted is not requested")
}

func testDifferentNS(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -i -n %s -N %s --source source --dest dest", migrateCmdline, ns1, ns2)
	require.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns2, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns2, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

// testFailWithoutNetworkPolicies tests that the migration fails if network policies are not enabled.
//
// For this test to work as expected, the cluster MUST use a CNI with NetworkPolicy support,
// AND it must be configured to block traffic across namespaces by default (unless an allowing NetworkPolicy is present).
//
// For example, Cilium with "policyEnforcementMode=always" (what we do in CI) meets these requirements:
// See: https://docs.cilium.io/en/stable/security/network/policyenforcement/
func testFailWithoutNetworkPolicies(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("--log-level debug --log-format json -i -n %s -N %s --source source --dest dest", ns1, ns2)
	require.Error(t, runCliApp(ctx, cmd), "migration was expected to have failed without NetworkPolicies - "+
		"does the cluster have a CNI that supports them and it is configured to enforce them?")
}

func testLbSvcDestHostOverride(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	// Create a service that will be used for the override
	svcName := "alternative-svc"
	_, err := mainClusterCli.KubeClient.CoreV1().Services(ns1).Create(context.Background(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:   svcName,
				Labels: resourceLabels,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app.kubernetes.io/component": "sshd",
					"app.kubernetes.io/name":      "pv-migrate",
				},
				Ports: []corev1.ServicePort{
					{
						Name:       "ssh",
						Port:       22,
						TargetPort: intstr.FromInt32(22),
					},
				},
			},
		}, metav1.CreateOptions{})
	require.NoError(t, err)

	// Prepare the destination with an extra file to test it remains after migration
	_, err = execInPod(ctx, mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	// Set the destination host override to use our custom service
	destHostOverride := svcName + "." + ns1
	cmd := fmt.Sprintf(
		"%s -i -n %s -N %s -H %s source dest", migrateLegacyCmdline, ns1, ns2, destHostOverride)
	require.NoError(t, runCliApp(ctx, cmd))

	// Verify the data was migrated correctly
	stdout, err := execInPod(ctx, mainClusterCli, ns2, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	// Check that ownership and content were preserved
	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	// Verify that the extra file still exists (no deletion)
	_, err = execInPod(ctx, mainClusterCli, ns2, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

func testRSA(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -a rsa -i -n %s -N %s source dest", migrateLegacyCmdline, ns1, ns2)
	require.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns2, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns2, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

func testDifferentCluster(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, extraClusterCli, ns3, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -K %s -i -n %s -N %s source dest", migrateLegacyCmdline,
		extraClusterKubeconfig, ns1, ns3)
	require.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, extraClusterCli, ns3, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, extraClusterCli, ns3, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

func testLocal(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, extraClusterCli, ns3, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -K %s -s local -i -n %s -N %s source dest", migrateLegacyCmdline,
		extraClusterKubeconfig, ns1, ns3)
	require.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, extraClusterCli, ns3, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, extraClusterCli, ns3, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

func testLongPVCNames(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns1, "long-dest", clearDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -i -n %s -N %s %s %s",
		migrateLegacyCmdline, ns1, ns1, longSourcePvcName, longDestPvcName)
	require.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns1, "long-dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])
}

func setup(t *testing.T, logger *slog.Logger) {
	logger.Info("set up integration tests")

	usr, err := user.Current()
	require.NoError(t, err)

	homeDir := usr.HomeDir

	extraClusterKubeconfig = env.GetString("PVMIG_TEST_EXTRA_KUBECONFIG", homeDir+"/.kube/config")

	mainCli, err := k8s.GetClusterClient("", "", logger)
	require.NoError(t, err)

	mainClusterCli = mainCli

	extraCli, err := k8s.GetClusterClient(extraClusterKubeconfig, "", logger)
	require.NoError(t, err)

	extraClusterCli = extraCli

	if mainCli.RestConfig.Host == extraCli.RestConfig.Host {
		logger.Warn("WARNING: USING A SINGLE CLUSTER FOR INTEGRATION TESTS!")
	}

	ns1 = "pv-migrate-test-1-" + util.RandomHexadecimalString(5)
	ns2 = "pv-migrate-test-2-" + util.RandomHexadecimalString(5)
	ns3 = "pv-migrate-test-3-" + util.RandomHexadecimalString(5)

	createNS(t, mainClusterCli, ns1)
	createNS(t, mainClusterCli, ns2)
	createNS(t, extraClusterCli, ns3)

	createPVC(t, mainClusterCli, ns1, longSourcePvcName)
	createPVC(t, mainClusterCli, ns1, longDestPvcName)
	createPVC(t, mainClusterCli, ns1, "source")
	createPVC(t, mainClusterCli, ns1, "dest")
	createPVC(t, mainClusterCli, ns2, "dest")
	createPVC(t, extraClusterCli, ns3, "dest")

	createPod(t, mainClusterCli, ns1, "long-source", longSourcePvcName)
	createPod(t, mainClusterCli, ns1, "long-dest", longDestPvcName)
	createPod(t, mainClusterCli, ns1, "source", "source")
	createPod(t, mainClusterCli, ns1, "dest", "dest")
	createPod(t, mainClusterCli, ns2, "dest", "dest")
	createPod(t, extraClusterCli, ns3, "dest", "dest")

	waitUntilPodIsRunning(t, mainClusterCli, ns1, "long-source")
	waitUntilPodIsRunning(t, mainClusterCli, ns1, "long-dest")
	waitUntilPodIsRunning(t, mainClusterCli, ns1, "source")
	waitUntilPodIsRunning(t, mainClusterCli, ns1, "dest")
	waitUntilPodIsRunning(t, mainClusterCli, ns2, "dest")
	waitUntilPodIsRunning(t, extraClusterCli, ns3, "dest")

	ctx := t.Context()

	_, err = execInPod(ctx, mainClusterCli, ns1, "long-source", generateDataShellCommand)
	require.NoError(t, err)

	_, err = execInPod(ctx, mainClusterCli, ns1, "source", generateDataShellCommand)
	require.NoError(t, err)

	logger.Info("set up integration tests done")
}

func teardownOnCleanup(t *testing.T, logger *slog.Logger) {
	t.Cleanup(func() {
		logger.Info("tear down integration tests")

		teardownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		deleteNS(teardownCtx, t, mainClusterCli, ns1)
		deleteNS(teardownCtx, t, mainClusterCli, ns2)
		deleteNS(teardownCtx, t, extraClusterCli, ns3)

		logger.Info("tear down integration tests done")
	})
}

func createPod(t *testing.T, cli *k8s.ClusterClient, namespace string, name string, pvc string) {
	terminationGracePeriodSeconds := int64(0)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    resourceLabels,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
			Volumes: []corev1.Volume{
				{
					Name: "volume",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "main",
					Image:   "docker.io/busybox:stable",
					Command: []string{"tail", "-f", "/dev/null"},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "volume",
							MountPath: "/volume",
						},
					},
				},
			},
		},
	}

	ctx := t.Context()

	_, err := cli.KubeClient.CoreV1().Pods(namespace).Create(ctx, &pod, metav1.CreateOptions{})
	require.NoError(t, err)
}

func createPVC(t *testing.T, cli *k8s.ClusterClient, namespace string, name string) {
	var storageClassRef *string

	storageClass := os.Getenv("PVMIG_TEST_STORAGE_CLASS")
	if storageClass != "" {
		storageClassRef = &storageClass
	}

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    resourceLabels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: storageClassRef,
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					"storage": resource.MustParse("64Mi"),
				},
			},
		},
	}

	_, err := cli.KubeClient.CoreV1().PersistentVolumeClaims(namespace).Create(t.Context(), &pvc, metav1.CreateOptions{})
	require.NoError(t, err)
}

//nolint:dupl
func waitUntilPodIsRunning(t *testing.T, cli *k8s.ClusterClient, namespace string, name string) {
	resCli := cli.KubeClient.CoreV1().Pods(namespace)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Minute)
	defer cancel()

	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fieldSelector

			list, err := resCli.List(t.Context(), options)
			if err != nil {
				return nil, fmt.Errorf("failed to list pods: %w", err)
			}

			return list, nil
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = fieldSelector

			cliWatch, err := resCli.Watch(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to watch pods: %w", err)
			}

			return cliWatch, nil
		},
	}

	_, err := watchtools.UntilWithSync(ctx, listWatch, &corev1.Pod{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Pod)
			require.Truef(t, ok, "unexpected type while watching pod %T: %s/%s", event.Object, namespace, name)

			return res.Status.Phase == corev1.PodRunning, nil
		})
	require.NoError(t, err)
}

func execInPod(ctx context.Context, cli *k8s.ClusterClient, ns string, name string, cmd string) (string, error) {
	stdoutBuffer := new(bytes.Buffer)
	stderrBuffer := new(bytes.Buffer)

	req := cli.KubeClient.CoreV1().RESTClient().Post().Resource("pods").
		Name(name).Namespace(ns).SubResource("exec").VersionedParams(
		&corev1.PodExecOptions{
			Command: []string{"sh", "-c", cmd},
			Stdin:   false,
			Stdout:  true,
			Stderr:  true,
			TTY:     false,
		},
		scheme.ParameterCodec,
	)

	config, err := cli.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return "", fmt.Errorf("failed to get REST config: %w", err)
	}

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var result *multierror.Error

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: stdoutBuffer, Stderr: stderrBuffer})
	if err != nil {
		result = multierror.Append(result, err)
	}

	stdout := stdoutBuffer.String()
	stderr := stderrBuffer.String()

	if stderr != "" {
		result = multierror.Append(result, fmt.Errorf("%w: %s", ErrPodExecStderr, stderr))
	}

	if err = result.ErrorOrNil(); err != nil {
		return "", fmt.Errorf("failed to execute command: %w", err)
	}

	return stdout, nil
}

func clearDestsOnCleanup(t *testing.T) {
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		_, err := execInPod(cleanupCtx, mainClusterCli, ns1, "dest", clearDataShellCommand)
		require.NoError(t, err)

		_, err = execInPod(cleanupCtx, mainClusterCli, ns2, "dest", clearDataShellCommand)
		require.NoError(t, err)

		_, err = execInPod(cleanupCtx, extraClusterCli, ns3, "dest", clearDataShellCommand)
		require.NoError(t, err)
	})
}

func createNS(t *testing.T, cli *k8s.ClusterClient, name string) {
	_, err := cli.KubeClient.CoreV1().
		Namespaces().Create(t.Context(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: resourceLabels,
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
}

func deleteNS(ctx context.Context, t *testing.T, cli *k8s.ClusterClient, name string) {
	namespaces := cli.KubeClient.CoreV1().Namespaces()
	require.NoError(t, namespaces.Delete(ctx, name, metav1.DeleteOptions{}))
}

func runCliApp(ctx context.Context, cmd string) error {
	return runCliAppWithArgs(ctx, strings.Fields(cmd)...)
}

func runCliAppWithArgs(ctx context.Context, args ...string) error {
	cliApp := app.BuildMigrateCmd(ctx, "", "", "", false)
	cliApp.SetArgs(args)

	if err := cliApp.Execute(); err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	return nil
}
