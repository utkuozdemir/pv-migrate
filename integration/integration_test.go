//go:build integration

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
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/neilotoole/slogt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/remotecommand"
	watchtools "k8s.io/client-go/tools/watch"
	"k8s.io/klog/v2"
	"k8s.io/utils/env"

	"github.com/utkuozdemir/pv-migrate/internal/app"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
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
)

var (
	generateDataShellCommand = fmt.Sprintf("echo -n %s > %s && chown %s:%s %s",
		generateDataContent, dataFilePath, dataFileUID, dataFileGID, dataFilePath)

	// generateRestrictedDataShellCommand creates root-owned files with permissions
	// that are inaccessible to non-root rsync but work fine as root:
	// - file with mode 0600 (unreadable by non-root even with fsGroup)
	// - directory with mode 0700 containing a file (untraversable by non-root).
	generateRestrictedDataShellCommand = "echo -n PRIVATE > /volume/root_only.txt && " +
		"chmod 0600 /volume/root_only.txt && " +
		"mkdir -p /volume/restricted_dir && " +
		"echo -n SECRET > /volume/restricted_dir/hidden.txt && " +
		"chmod 0700 /volume/restricted_dir"

	checkRestrictedDataShellCommand = "cat /volume/root_only.txt && " +
		"cat /volume/restricted_dir/hidden.txt"

	generateExtraDataShellCommand = fmt.Sprintf("echo -n %s > %s",
		generateDataContent, extraDataFilePath)
	printDataUIDGIDContentShellCommand = fmt.Sprintf(
		"stat -c '%%u' %s && stat -c '%%g' %s && cat %s",
		dataFilePath,
		dataFilePath,
		dataFilePath,
	)
	checkExtraDataShellCommand = "ls " + extraDataFilePath
	resourceLabels             = map[string]string{
		"pv-migrate-test": "true",
	}

	ErrPodExecStderr = errors.New("pod exec stderr")
)

// sharedInfra holds cluster clients and the shared read-only source namespace,
// created once per test run. Tests that only read from source share this.
type sharedInfra struct {
	mainCli         *k8s.ClusterClient
	extraCli        *k8s.ClusterClient
	extraKubeconfig string
	sourceNS        string
}

// testEnv holds per-test infrastructure: isolated namespace(s) for each subtest.
type testEnv struct {
	shared    *sharedInfra
	sourceNS  string
	destNS    string
	sourceCli *k8s.ClusterClient
	destCli   *k8s.ClusterClient
}

//nolint:funlen
func TestIntegration(t *testing.T) {
	t.Parallel()

	si := setupShared(t)

	t.Run("SameNS", func(t *testing.T) {
		t.Parallel()
		te := setupSameNS(t, si)
		testSameNS(t, te)
	})
	t.Run("RsyncExtraArgs", func(t *testing.T) {
		t.Parallel()
		testRsyncExtraArgs(t, si)
	})
	t.Run("LoadBalancer", func(t *testing.T) {
		t.Parallel()
		te := setupExtraCluster(t, si)
		testLoadBalancer(t, te)
	})
	t.Run("NoChown", func(t *testing.T) {
		t.Parallel()
		te := setupSameNS(t, si)
		testNoChown(t, te)
	})
	t.Run("DeleteExtraneousFiles", func(t *testing.T) {
		t.Parallel()
		te := setupSameNS(t, si)
		testDeleteExtraneousFiles(t, te)
	})
	t.Run("MountedError", func(t *testing.T) {
		t.Parallel()
		te := setupSameNS(t, si)
		testMountedError(t, te)
	})
	t.Run("DifferentNS", func(t *testing.T) {
		t.Parallel()
		te := setupIsolatedDiffNS(t, si, generateDataShellCommand+" && "+generateRestrictedDataShellCommand)
		testDifferentNS(t, te)
	})
	t.Run("FailWithoutNetworkPolicies", func(t *testing.T) {
		t.Parallel()
		te := setupDiffNS(t, si)
		testFailWithoutNetworkPolicies(t, te)
	})
	t.Run("LoadBalancerDestHostOverride", func(t *testing.T) {
		t.Parallel()
		te := setupIsolatedDiffNS(t, si, generateDataShellCommand)
		testLoadBalancerDestHostOverride(t, te)
	})
	t.Run("RSA", func(t *testing.T) {
		t.Parallel()
		te := setupDiffNS(t, si)
		testRSA(t, te)
	})
	t.Run("DifferentCluster", func(t *testing.T) {
		t.Parallel()
		te := setupExtraCluster(t, si)
		testDifferentCluster(t, te)
	})
	t.Run("Local", func(t *testing.T) {
		t.Parallel()
		te := setupExtraCluster(t, si)
		testLocal(t, te)
	})
	t.Run("LongPVCNames", func(t *testing.T) {
		t.Parallel()
		te := setupLongPVCNames(t, si)
		testLongPVCNames(t, te)
	})
	t.Run("NodePort", func(t *testing.T) {
		t.Parallel()
		te := setupExtraCluster(t, si)
		testNodePort(t, te)
	})
	t.Run("NodePortDestHostOverride", func(t *testing.T) {
		t.Parallel()
		te := setupIsolatedDiffNS(t, si, generateDataShellCommand)
		testNodePortDestHostOverride(t, te)
	})
	t.Run("NodePortCustomPort", func(t *testing.T) {
		t.Parallel()
		te := setupExtraCluster(t, si)
		testNodePortCustomPort(t, te)
	})
	t.Run("NonRoot", func(t *testing.T) {
		t.Parallel()
		te := setupDiffNS(t, si)
		testNonRoot(t, te)
	})
	t.Run("NonRootFailOnRestrictedFiles", func(t *testing.T) {
		t.Parallel()
		te := setupIsolatedDiffNS(t, si, generateDataShellCommand+" && "+generateRestrictedDataShellCommand)
		testNonRootFailOnRestrictedFiles(t, te)
	})
	t.Run("DetachMode", func(t *testing.T) {
		t.Parallel()
		te := setupSameNS(t, si)
		testDetachMode(t, te)
	})

	// Push mode tests
	t.Run("ClusterIPPush", func(t *testing.T) {
		t.Parallel()
		te := setupDiffNS(t, si)
		testClusterIPPush(t, te)
	})
	t.Run("LoadBalancerPush", func(t *testing.T) {
		t.Parallel()
		te := setupExtraCluster(t, si)
		testLoadBalancerPush(t, te)
	})
	t.Run("NodePortPush", func(t *testing.T) {
		t.Parallel()
		te := setupExtraCluster(t, si)
		testNodePortPush(t, te)
	})
}

// --- Test functions ---

//nolint:dupl,thelper
func testSameNS(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -i -n %s -N %s --source source --dest dest", defaultHelmArgs(t), te.sourceNS, te.destNS)
	require.NoError(t, runCliApp(ctx, t, cmd))

	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

// testRsyncExtraArgs verifies that --rsync-extra-args passes flags through to rsync.
// It uses --log-file to make rsync write a log file, then asserts the file exists.
// Two sub-tests cover Job-based (clusterip) and SSH-based (local) strategies.
// The log file path and check pod differ because rsync runs in different places:
// in Job-based strategies it runs on the dest side, in local it runs on the source side.
//
//nolint:thelper
func testRsyncExtraArgs(t *testing.T, si *sharedInfra) {
	tests := []struct {
		name        string
		extraFlags  string
		logFilePath string // path as seen by the rsync process
		checkPod    string // test pod where the log file ends up
	}{
		{
			name:        "ClusterIP",
			extraFlags:  "-s clusterip",
			logFilePath: "/dest/rsync.log",
			checkPod:    "dest",
		},
		{
			name:        "Local",
			extraFlags:  "-s local -R",
			logFilePath: "/source/rsync.log",
			checkPod:    "source",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := setupSameNS(t, si)
			ctx := t.Context()

			_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
			require.NoError(t, err)

			cmd := fmt.Sprintf("%s -i -n %s -N %s --rsync-extra-args=--log-file=%s --source source --dest dest",
				defaultHelmArgs(t), te.sourceNS, te.destNS, tc.logFilePath)
			if tc.extraFlags != "" {
				cmd += " " + tc.extraFlags
			}

			require.NoError(t, runCliApp(ctx, t, cmd))

			stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
			require.NoError(t, err)

			parts := strings.Split(stdout, "\n")
			assert.Len(t, parts, 3)

			if len(parts) < 3 {
				return
			}

			assert.Equal(t, dataFileUID, parts[0])
			assert.Equal(t, dataFileGID, parts[1])
			assert.Equal(t, generateDataContent, parts[2])

			_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
			require.NoError(t, err)

			// Verify rsync actually received the extra arg by checking the log file it wrote.
			_, err = execInPod(ctx, te.sourceCli, te.sourceNS, tc.checkPod, "test -f /volume/rsync.log")
			require.NoError(t, err)

			t.Log("Verified custom rsync extra args were passed through and log file was created")
		})
	}
}

//nolint:dupl,thelper
func testLoadBalancer(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf(
		"%s -K %s -s loadbalancer -i -n %s -N %s --loadbalancer-timeout 5m --source source --dest dest",
		defaultHelmArgs(t),
		te.shared.extraKubeconfig,
		te.sourceNS,
		te.destNS,
	)
	require.NoError(t, runCliApp(ctx, t, cmd))

	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

//nolint:thelper
func testNoChown(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -i -o -n %s -N %s --source source --dest dest", defaultHelmArgs(t), te.sourceNS, te.destNS)
	require.NoError(t, runCliApp(ctx, t, cmd))

	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, "0", parts[0])
	assert.Equal(t, "0", parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

//nolint:thelper
func testDeleteExtraneousFiles(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s --no-compress -d -i -n %s -N %s --source source --dest dest",
		defaultHelmArgs(t), te.sourceNS, te.destNS)
	require.NoError(t, runCliApp(ctx, t, cmd))

	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No such file or directory")
}

//nolint:thelper
func testMountedError(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -n %s -N %s --source source --dest dest", defaultHelmArgs(t), te.sourceNS, te.destNS)
	err = runCliApp(ctx, t, cmd)
	assert.ErrorContains(t, err, "ignore-mounted is not requested")
}

//nolint:thelper
func testDifferentNS(t *testing.T, te *testEnv) {
	ctx := t.Context()

	// Restricted source data is already seeded via init container.

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	podWatchCancel := startPodWatches(t, te.sourceCli, te.sourceNS, te.destNS)

	cmd := fmt.Sprintf(
		"%s -i -n %s -N %s --source source --dest dest",
		defaultHelmArgs(t),
		te.sourceNS,
		te.destNS,
	)
	require.NoError(t, runCliApp(ctx, t, cmd))

	createdPods := podWatchCancel()
	assertImages(t, createdPods)

	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	// Verify restricted files were migrated (root can read everything)
	stdout, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkRestrictedDataShellCommand)
	require.NoError(t, err)
	assert.Equal(t, "PRIVATESECRET", stdout)

	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

// testFailWithoutNetworkPolicies tests that the migration fails if network policies are not enabled.
//
// For this test to work as expected, the cluster MUST use a CNI with NetworkPolicy support,
// AND it must be configured to block traffic across namespaces by default
// (unless an allowing NetworkPolicy is present).
//
// For example, Cilium with "policyEnforcementMode=always" (what we do in CI) meets these requirements:
// See: https://docs.cilium.io/en/stable/security/network/policyenforcement/
//
//nolint:thelper
func testFailWithoutNetworkPolicies(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	// Force a single strategy, zero retries, and a short helm timeout to fail fast -- we only
	// need to confirm that the migration fails without NetworkPolicies.
	cmd := fmt.Sprintf(
		"%s --no-cleanup-on-failure --helm-set rsync.ttlSecondsAfterFinished=3600"+
			" --helm-set rsync.maxRetries=0 --helm-timeout 30s --log-level debug --log-format json"+
			" -s clusterip -i -n %s -N %s --source source --dest dest",
		imageHelmArgs(t),
		te.sourceNS,
		te.destNS,
	)
	require.Error(
		t,
		runCliApp(ctx, t, cmd),
		"migration was expected to have failed without NetworkPolicies - "+
			"does the cluster have a CNI that supports them and it is configured to enforce them?",
	)
}

//nolint:thelper
func testLoadBalancerDestHostOverride(t *testing.T, te *testEnv) {
	ctx := t.Context()

	// Create a service that will be used for the override
	svcName := "alternative-svc"
	_, err := te.sourceCli.KubeClient.CoreV1().Services(te.sourceNS).Create(ctx,
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
	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	// Set the destination host override to use our custom service
	destHostOverride := svcName + "." + te.sourceNS
	cmd := fmt.Sprintf(
		"%s -i -n %s -N %s -H %s --source source --dest dest",
		defaultHelmArgs(t), te.sourceNS, te.destNS, destHostOverride)
	require.NoError(t, runCliApp(ctx, t, cmd))

	// Verify the data was migrated correctly
	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	// Check that ownership and content were preserved
	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	// Verify that the extra file still exists (no deletion)
	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

//nolint:dupl,thelper
func testRSA(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -a rsa -i -n %s -N %s --source source --dest dest",
		defaultHelmArgs(t), te.sourceNS, te.destNS)
	require.NoError(t, runCliApp(ctx, t, cmd))

	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

//nolint:dupl,thelper
func testDifferentCluster(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -K %s -i -n %s -N %s --source source --dest dest", defaultHelmArgs(t),
		te.shared.extraKubeconfig, te.sourceNS, te.destNS)
	require.NoError(t, runCliApp(ctx, t, cmd))

	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

//nolint:dupl,thelper
func testLocal(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf("%s -K %s -s local -i -n %s -N %s --source source --dest dest", defaultHelmArgs(t),
		te.shared.extraKubeconfig, te.sourceNS, te.destNS)
	require.NoError(t, runCliApp(ctx, t, cmd))

	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

//nolint:thelper
func testLongPVCNames(t *testing.T, te *testEnv) {
	ctx := t.Context()

	cmd := fmt.Sprintf("%s -i -n %s -N %s --source %s --dest %s",
		defaultHelmArgs(t), te.sourceNS, te.destNS, longSourcePvcName, longDestPvcName)
	require.NoError(t, runCliApp(ctx, t, cmd))

	stdout, err := execInPod(
		ctx,
		te.destCli,
		te.destNS,
		"long-dest",
		printDataUIDGIDContentShellCommand,
	)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])
}

// testNodePort tests the NodePort strategy in the same namespace.
//
//nolint:dupl,thelper
func testNodePort(t *testing.T, te *testEnv) {
	ctx := t.Context()

	// Prepare the destination with an extra file to test it remains after migration
	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	// Run the migration using the NodePort strategy specifically
	cmd := fmt.Sprintf("%s -K %s -s nodeport -i -n %s -N %s --source source --dest dest",
		defaultHelmArgs(t), te.shared.extraKubeconfig, te.sourceNS, te.destNS)
	require.NoError(t, runCliApp(ctx, t, cmd))

	// Verify the data was migrated correctly
	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	// Check that ownership and content were preserved
	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	// Verify that the extra file still exists (no deletion)
	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

// testNodePortDestHostOverride tests the NodePort strategy with a custom destination host override.
//
//nolint:thelper
func testNodePortDestHostOverride(t *testing.T, te *testEnv) {
	ctx := t.Context()

	// Define a custom NodePort in the valid range
	customNodePort := 30022

	// Create a service that will be used for the override
	svcName := "nodeport-override-svc"
	_, err := te.sourceCli.KubeClient.CoreV1().Services(te.sourceNS).Create(ctx,
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:   svcName,
				Labels: resourceLabels,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Selector: map[string]string{
					"app.kubernetes.io/component": "sshd",
					"app.kubernetes.io/name":      "pv-migrate",
				},
				Ports: []corev1.ServicePort{
					{
						Name:       "ssh",
						Port:       int32(customNodePort),
						TargetPort: intstr.FromInt32(22),
					},
				},
			},
		}, metav1.CreateOptions{})
	require.NoError(t, err)

	// Prepare the destination with an extra file to test it remains after migration
	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	// Set the destination host override to use our custom service
	destHostOverride := svcName + "." + te.sourceNS
	cmd := fmt.Sprintf(
		"%s -s nodeport --helm-set sshd.service.nodePort=%d -i -n %s -N %s -H %s --source source --dest dest",
		defaultHelmArgs(t), customNodePort, te.sourceNS, te.destNS, destHostOverride)
	require.NoError(t, runCliApp(ctx, t, cmd))

	// Verify the data was migrated correctly
	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	// Check that ownership and content were preserved
	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	// Verify that the extra file still exists (no deletion)
	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

// testNodePortCustomPort tests the NodePort strategy with a custom NodePort port.
//
//nolint:thelper
func testNodePortCustomPort(t *testing.T, te *testEnv) {
	ctx := t.Context()

	// Define a custom NodePort in the valid range
	customNodePort := 31234

	// Prepare the destination with an extra file to test it remains after migration
	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	// Run the migration using the NodePort strategy with a custom port
	cmd := fmt.Sprintf(
		"%s -K %s -s nodeport --helm-set sshd.service.nodePort=%d -i -n %s -N %s --source source --dest dest",
		defaultHelmArgs(t), te.shared.extraKubeconfig, customNodePort, te.sourceNS, te.destNS,
	)
	require.NoError(t, runCliApp(ctx, t, cmd))

	// Verify the data was migrated correctly
	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Len(t, parts, 3)

	if len(parts) < 3 {
		return
	}

	// Check that ownership and content were preserved
	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	// Verify that the extra file still exists (no deletion)
	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

//nolint:thelper
func testNonRoot(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf(
		"%s --non-root -i -n %s -N %s --source source --dest dest",
		defaultHelmArgs(t),
		te.sourceNS,
		te.destNS,
	)
	require.NoError(t, runCliApp(ctx, t, cmd))

	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", "cat /volume/file.txt")
	require.NoError(t, err)
	assert.Equal(t, generateDataContent, stdout)

	_, err = execInPod(ctx, te.destCli, te.destNS, "dest", checkExtraDataShellCommand)
	require.NoError(t, err)
}

//nolint:thelper
func testNonRootFailOnRestrictedFiles(t *testing.T, te *testEnv) {
	ctx := t.Context()

	// Restricted source data is already seeded via init container.

	// Force a single strategy, zero retries, and a short helm timeout to fail fast -- we only
	// need to confirm that non-root rsync fails on restricted files.
	cmd := fmt.Sprintf(
		"%s --helm-set rsync.maxRetries=0 --helm-timeout 30s"+
			" --non-root -s clusterip -i -n %s -N %s --source source --dest dest",
		defaultHelmArgs(t),
		te.sourceNS,
		te.destNS,
	)
	require.Error(t, runCliApp(ctx, t, cmd))
}

//nolint:thelper
func testDetachMode(t *testing.T, te *testEnv) {
	ctx := t.Context()
	migrationID := "detach-test"

	// Run the migration in detach mode with a custom ID
	cmd := fmt.Sprintf("%s -i -n %s -N %s --source source --dest dest --detach --id %s",
		defaultHelmArgs(t), te.sourceNS, te.destNS, migrationID)
	require.NoError(t, runCliApp(ctx, t, cmd))

	// Find the rsync job created by the detached migration
	releasePrefix := "pv-migrate-" + migrationID + "-"
	job, err := k8s.FindRsyncJob(ctx, te.sourceCli.KubeClient, te.sourceNS, releasePrefix)
	require.NoError(t, err)
	assert.Contains(t, job.Name, migrationID)

	// Wait for the job to complete
	require.Eventually(t, func() bool {
		j, err := te.sourceCli.KubeClient.BatchV1().Jobs(job.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
		if err != nil {
			return false
		}

		return j.Status.Succeeded > 0 || j.Status.Failed > 0
	}, 2*time.Minute, 2*time.Second, "rsync job did not complete in time")

	// Run status (without --follow) and verify it doesn't error
	require.NoError(t, runCliAppWithArgs(ctx, t, "status", "-n", te.sourceNS, migrationID))

	// Verify that the data was actually migrated
	stdout, err := execInPod(ctx, te.destCli, te.destNS, "dest", printDataUIDGIDContentShellCommand)
	require.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	require.Len(t, parts, 3)

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	// Clean up using the cleanup subcommand
	require.NoError(t, runCliAppWithArgs(ctx, t, "cleanup", "-n", te.sourceNS, "--force", migrationID))

	// Verify the Helm releases are gone by running cleanup again (should error: no releases found)
	err = runCliAppWithArgs(ctx, t, "cleanup", "-n", te.sourceNS, migrationID)
	require.ErrorContains(t, err, "no releases found")
}

// --- Setup helpers ---

func setupShared(t *testing.T) *sharedInfra {
	t.Helper()

	logger := slogt.New(t)
	slog.SetDefault(logger)
	klog.SetSlogLogger(logger)

	usr, err := user.Current()
	require.NoError(t, err)

	extraKubeconfig := env.GetString("PVMIG_TEST_EXTRA_KUBECONFIG", usr.HomeDir+"/.kube/config")

	mainCli, err := k8s.GetClusterClient("", "", logger)
	require.NoError(t, err)

	extraCli, err := k8s.GetClusterClient(extraKubeconfig, "", logger)
	require.NoError(t, err)

	if mainCli.RestConfig.Host == extraCli.RestConfig.Host {
		logger.Warn("WARNING: USING A SINGLE CLUSTER FOR INTEGRATION TESTS!")
	}

	logger.Info("setting up shared source namespace")

	sourceNS := newTestNS(t, mainCli, "pvmig-src")
	require.NoError(t, provisionPod(t.Context(), mainCli, sourceNS, "source", "source", generateDataShellCommand))

	logger.Info("shared source namespace ready", "ns", sourceNS)

	return &sharedInfra{
		mainCli:         mainCli,
		extraCli:        extraCli,
		extraKubeconfig: extraKubeconfig,
		sourceNS:        sourceNS,
	}
}

// setupSameNS creates a per-test namespace with both source and dest PVCs+pods.
func setupSameNS(t *testing.T, si *sharedInfra) *testEnv {
	t.Helper()

	ns := newTestNS(t, si.mainCli, "pvmig-test")

	eg, ctx := errgroup.WithContext(t.Context())
	eg.Go(func() error {
		return provisionPod(ctx, si.mainCli, ns, "source", "source", generateDataShellCommand)
	})
	eg.Go(func() error {
		return provisionPod(ctx, si.mainCli, ns, "dest", "dest", "")
	})
	require.NoError(t, eg.Wait())

	return &testEnv{shared: si, sourceNS: ns, destNS: ns, sourceCli: si.mainCli, destCli: si.mainCli}
}

// setupDiffNS uses the shared source and creates a per-test dest namespace.
func setupDiffNS(t *testing.T, si *sharedInfra) *testEnv {
	t.Helper()

	destNS := newTestNS(t, si.mainCli, "pvmig-test")
	require.NoError(t, provisionPod(t.Context(), si.mainCli, destNS, "dest", "dest", ""))

	return &testEnv{shared: si, sourceNS: si.sourceNS, destNS: destNS, sourceCli: si.mainCli, destCli: si.mainCli}
}

// setupIsolatedDiffNS creates per-test source and dest namespaces.
// The source pod is seeded with seedCmd via init container.
func setupIsolatedDiffNS(t *testing.T, si *sharedInfra, seedCmd string) *testEnv {
	t.Helper()

	sourceNS := newTestNS(t, si.mainCli, "pvmig-test")
	destNS := newTestNS(t, si.mainCli, "pvmig-test")

	eg, ctx := errgroup.WithContext(t.Context())
	eg.Go(func() error {
		return provisionPod(ctx, si.mainCli, sourceNS, "source", "source", seedCmd)
	})
	eg.Go(func() error {
		return provisionPod(ctx, si.mainCli, destNS, "dest", "dest", "")
	})
	require.NoError(t, eg.Wait())

	return &testEnv{shared: si, sourceNS: sourceNS, destNS: destNS, sourceCli: si.mainCli, destCli: si.mainCli}
}

// setupExtraCluster uses shared source on main cluster and per-test dest on extra cluster.
func setupExtraCluster(t *testing.T, si *sharedInfra) *testEnv {
	t.Helper()

	destNS := newTestNS(t, si.extraCli, "pvmig-test")
	require.NoError(t, provisionPod(t.Context(), si.extraCli, destNS, "dest", "dest", ""))

	return &testEnv{shared: si, sourceNS: si.sourceNS, destNS: destNS, sourceCli: si.mainCli, destCli: si.extraCli}
}

// setupLongPVCNames creates a per-test namespace with long-named PVCs.
func setupLongPVCNames(t *testing.T, si *sharedInfra) *testEnv {
	t.Helper()

	ns := newTestNS(t, si.mainCli, "pvmig-test")

	eg, ctx := errgroup.WithContext(t.Context())
	eg.Go(func() error {
		return provisionPod(ctx, si.mainCli, ns, longSourcePvcName, "long-source", generateDataShellCommand)
	})
	eg.Go(func() error {
		return provisionPod(ctx, si.mainCli, ns, longDestPvcName, "long-dest", "")
	})
	require.NoError(t, eg.Wait())

	return &testEnv{shared: si, sourceNS: ns, destNS: ns, sourceCli: si.mainCli, destCli: si.mainCli}
}

// --- Infrastructure helpers ---

// newTestNS creates a namespace with a random suffix and registers fire-and-forget cleanup.
func newTestNS(t *testing.T, cli *k8s.ClusterClient, prefix string) string {
	t.Helper()

	name := prefix + "-" + utilrand.String(5)

	_, err := cli.KubeClient.CoreV1().Namespaces().Create(t.Context(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: resourceLabels,
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("test failed, skipping cleanup of namespace %q for post-mortem inspection", name)

			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := cli.KubeClient.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			t.Logf("failed to delete test namespace %q: %v", name, err)
		}
	})

	return name
}

// provisionPod creates a PVC, a pod mounting it, and waits for the pod to be running.
// If seedCmd is non-empty, an init container seeds data before the main container starts.
// This function returns an error (instead of calling require) so it can be used in errgroup goroutines.
//
//nolint:funlen
func provisionPod(ctx context.Context, cli *k8s.ClusterClient, ns, pvcName, podName, seedCmd string) error {
	// Create PVC
	var storageClassRef *string
	if sc := os.Getenv("PVMIG_TEST_STORAGE_CLASS"); sc != "" {
		storageClassRef = &sc
	}

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: ns,
			Labels:    resourceLabels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: storageClassRef,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					"storage": resource.MustParse("64Mi"),
				},
			},
		},
	}

	if _, err := cli.KubeClient.CoreV1().
		PersistentVolumeClaims(ns).
		Create(ctx, &pvc, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create PVC %s/%s: %w", ns, pvcName, err)
	}

	// Create pod
	terminationGracePeriodSeconds := int64(0)

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
			Labels:    resourceLabels,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
			Volumes: []corev1.Volume{
				{
					Name: "volume",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
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

	if seedCmd != "" {
		pod.Spec.InitContainers = []corev1.Container{
			{
				Name:    "seed",
				Image:   "docker.io/busybox:stable",
				Command: []string{"sh", "-c", seedCmd},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "volume",
						MountPath: "/volume",
					},
				},
			},
		}
	}

	if _, err := cli.KubeClient.CoreV1().Pods(ns).Create(ctx, &pod, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create pod %s/%s: %w", ns, podName, err)
	}

	// Wait for pod to be running
	return waitPodRunning(ctx, cli, ns, podName)
}

// waitPodRunning waits until a pod reaches the Running phase.
// Returns an error (instead of calling require) so it can be used in errgroup goroutines.
func waitPodRunning(ctx context.Context, cli *k8s.ClusterClient, ns, name string) error {
	resCli := cli.KubeClient.CoreV1().Pods(ns)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()

	listWatch := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fieldSelector

			list, err := resCli.List(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to list pods: %w", err)
			}

			return list, nil
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = fieldSelector

			cliWatch, err := resCli.Watch(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to watch pods: %w", err)
			}

			return cliWatch, nil
		},
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	_, err := watchtools.UntilWithSync(ctx, listWatch, &corev1.Pod{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Pod)
			if !ok {
				return false, fmt.Errorf("unexpected type %T watching pod %s/%s", event.Object, ns, name)
			}

			return res.Status.Phase == corev1.PodRunning, nil
		})
	if err != nil {
		return fmt.Errorf("wait for pod %s/%s running: %w", ns, name, err)
	}

	return nil
}

// --- Pod watches (used by testDifferentNS) ---

//nolint:nonamedreturns
func startPodWatches(t *testing.T, cli *k8s.ClusterClient, namespaces ...string) (cancelFunc func() []*corev1.Pod) {
	t.Helper()

	nsSet := make(map[string]struct{}, len(namespaces))
	for _, ns := range namespaces {
		nsSet[ns] = struct{}{}
	}

	factory := informers.NewSharedInformerFactoryWithOptions(
		cli.KubeClient,
		0,
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = "app.kubernetes.io/name=pv-migrate"
		}),
	)

	podInformer := factory.Core().V1().Pods().Informer()

	var (
		mu          sync.Mutex
		createdPods []*corev1.Pod
	)

	_, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				return
			}

			// Skip pods that are being deleted -- these are leftovers from previous tests
			// that the informer picks up during its initial List.
			if pod.DeletionTimestamp != nil {
				return
			}

			// Only track pods in this test's namespaces
			if _, inScope := nsSet[pod.Namespace]; !inScope {
				return
			}

			mu.Lock()

			createdPods = append(createdPods, pod)
			mu.Unlock()
		},
	})
	require.NoError(t, err)

	stopCh := make(chan struct{})

	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)

	return func() []*corev1.Pod {
		close(stopCh)
		factory.Shutdown()

		mu.Lock()
		defer mu.Unlock()

		return createdPods
	}
}

func assertImages(t *testing.T, pods []*corev1.Pod) {
	t.Helper()

	require.Len(t, pods, 2)

	expectedRsyncImage, expectedSshdImage := getImages(t)

	actualImages := make([]string, 0, len(pods))

	for _, pod := range pods {
		require.Len(t, pod.Spec.Containers, 1)

		actualImages = append(actualImages, pod.Spec.Containers[0].Image)
	}

	require.ElementsMatch(t, []string{expectedRsyncImage, expectedSshdImage}, actualImages)
}

// --- CLI and exec helpers ---

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

func runCliApp(ctx context.Context, t *testing.T, cmd string) error {
	t.Helper()

	return runCliAppWithArgs(ctx, t, strings.Fields(cmd)...)
}

func runCliAppWithArgs(ctx context.Context, t *testing.T, args ...string) error {
	t.Helper()

	t.Logf("running command: %s", strings.Join(args, " "))

	cliApp, err := app.BuildMigrateCmd(ctx, "", "", "", slogt.New(t))
	if err != nil {
		return fmt.Errorf("failed to build command: %w", err)
	}

	cliApp.SetArgs(args)

	if err = cliApp.Execute(); err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	return nil
}

func imageHelmArgs(t *testing.T) string {
	t.Helper()

	rsyncImage, sshdImage := getImages(t)

	rsyncImageParts := strings.SplitN(rsyncImage, ":", 2)
	require.Len(t, rsyncImageParts, 2)

	sshdImageParts := strings.SplitN(sshdImage, ":", 2)
	require.Len(t, sshdImageParts, 2)

	return "--helm-set rsync.image.repository=" + rsyncImageParts[0] + " " +
		"--helm-set rsync.image.tag=" + rsyncImageParts[1] + " " +
		"--helm-set sshd.image.repository=" + sshdImageParts[0] + " " +
		"--helm-set sshd.image.tag=" + sshdImageParts[1]
}

func defaultHelmArgs(t *testing.T) string {
	t.Helper()

	return "--no-cleanup-on-failure " +
		"--helm-set rsync.networkPolicy.enabled=true " +
		"--helm-set sshd.networkPolicy.enabled=true " +
		"--helm-set rsync.ttlSecondsAfterFinished=3600 " +
		imageHelmArgs(t)
}

//nolint:nonamedreturns
func getImages(t *testing.T) (rsyncImage, sshdImage string) {
	t.Helper()

	rsyncImage = os.Getenv("RSYNC_IMAGE")
	require.NotEmpty(t, rsyncImage, "RSYNC_IMAGE env var must be set")

	sshdImage = os.Getenv("SSHD_IMAGE")
	require.NotEmpty(t, sshdImage, "SSHD_IMAGE env var must be set")

	return rsyncImage, sshdImage
}
