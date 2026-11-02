package strategy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/common"
	commonutil "helm.sh/helm/v4/pkg/chart/common/util"
	"helm.sh/helm/v4/pkg/engine"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
)

func TestNonRootValuesPassRestrictedPodSecurity(t *testing.T) {
	t.Parallel()

	base := realisticClusterIPBase()
	req := &migration.Request{NonRoot: true}

	applyNonRootValues(base, req)

	merged, err := getMergedHelmValues(base, req)
	require.NoError(t, err)

	loaded, err := helm.LoadChart("")
	require.NoError(t, err)

	renderValues, err := commonutil.ToRenderValues(
		loaded,
		merged,
		common.ReleaseOptions{Name: "pv-migrate-test", Namespace: "source-ns"},
		nil,
	)
	require.NoError(t, err)

	files, err := engine.Render(loaded, renderValues)
	require.NoError(t, err)

	var sshdDeployment appsv1.Deployment
	require.NoError(t, yaml.Unmarshal([]byte(files["pv-migrate/templates/sshd/deployment.yaml"]), &sshdDeployment))
	assertRestrictedPodSpec(t, &sshdDeployment.Spec.Template.Spec, "sshd")

	var rsyncJob batchv1.Job
	require.NoError(t, yaml.Unmarshal([]byte(files["pv-migrate/templates/rsync/job.yaml"]), &rsyncJob))
	assertRestrictedPodSpec(t, &rsyncJob.Spec.Template.Spec, "rsync")
}

func assertRestrictedPodSpec(t *testing.T, podSpec *corev1.PodSpec, containerName string) {
	t.Helper()

	require.NotNil(t, podSpec.SecurityContext)
	require.NotNil(t, podSpec.SecurityContext.FSGroup)
	assert.Equal(t, int64(nonRootUID), *podSpec.SecurityContext.FSGroup)

	require.Len(t, podSpec.Containers, 1)
	assert.Equal(t, containerName, podSpec.Containers[0].Name)

	secCtx := podSpec.Containers[0].SecurityContext
	require.NotNil(t, secCtx)
	require.NotNil(t, secCtx.RunAsNonRoot)
	assert.True(t, *secCtx.RunAsNonRoot)
	require.NotNil(t, secCtx.RunAsUser)
	assert.Equal(t, int64(nonRootUID), *secCtx.RunAsUser)
	require.NotNil(t, secCtx.RunAsGroup)
	assert.Equal(t, int64(nonRootUID), *secCtx.RunAsGroup)
	require.NotNil(t, secCtx.AllowPrivilegeEscalation)
	assert.False(t, *secCtx.AllowPrivilegeEscalation)

	require.NotNil(t, secCtx.SeccompProfile)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, secCtx.SeccompProfile.Type)

	require.NotNil(t, secCtx.Capabilities)
	assert.Equal(t, []corev1.Capability{"ALL"}, secCtx.Capabilities.Drop)
	assert.Empty(t, secCtx.Capabilities.Add)
}
