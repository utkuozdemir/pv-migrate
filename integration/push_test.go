//go:build integration

//nolint:paralleltest
package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClusterIPPush(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf(
		"%s -s clusterip --rsync-push -i -n %s -N %s --source source --dest dest",
		defaultHelmArgs(t),
		ns1,
		ns2,
	)
	require.NoError(t, runCliApp(ctx, t, cmd))

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

func testLoadBalancerPush(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf(
		"%s -s loadbalancer --rsync-push -i -n %s -N %s --loadbalancer-timeout 5m --source source --dest dest",
		defaultHelmArgs(t),
		ns1,
		ns1,
	)
	require.NoError(t, runCliApp(ctx, t, cmd))

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

func testNodePortPush(t *testing.T) {
	clearDestsOnCleanup(t)
	ctx := t.Context()

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf(
		"%s -s nodeport --rsync-push -i -n %s -N %s --source source --dest dest",
		defaultHelmArgs(t),
		ns1,
		ns1,
	)
	require.NoError(t, runCliApp(ctx, t, cmd))

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
