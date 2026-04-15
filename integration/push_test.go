//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:dupl,thelper
func testClusterIPPush(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf(
		"%s -s clusterip --rsync-push -i -n %s -N %s --source source --dest dest",
		defaultHelmArgs(t),
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

//nolint:dupl,thelper
func testLoadBalancerPush(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf(
		"%s -K %s -s loadbalancer --rsync-push -i -n %s -N %s --loadbalancer-timeout 5m --source source --dest dest",
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

//nolint:dupl,thelper
func testNodePortPush(t *testing.T, te *testEnv) {
	ctx := t.Context()

	_, err := execInPod(ctx, te.destCli, te.destNS, "dest", generateExtraDataShellCommand)
	require.NoError(t, err)

	cmd := fmt.Sprintf(
		"%s -K %s -s nodeport --rsync-push -i -n %s -N %s --source source --dest dest",
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
