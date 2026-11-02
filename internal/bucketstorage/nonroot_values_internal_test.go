package bucketstorage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/common"
	commonutil "helm.sh/helm/v4/pkg/chart/common/util"
	"helm.sh/helm/v4/pkg/engine"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
)

func TestNonRootValuesPassRestrictedPodSecurity(t *testing.T) {
	t.Parallel()

	info := &pvc.Info{
		Claim: &corev1.PersistentVolumeClaim{Namespace: "ns", Name: "pvc"},
	}
	req := &Request{NonRoot: true}

	values := buildHelmValues("ns", req, info, "[remote]\ntype = s3\n", "rclone sync '/data' 'remote:b/'",
		true, "", "")

	merged, err := mergeHelmValues(values, req)
	require.NoError(t, err)

	loaded, err := helm.LoadChart("")
	require.NoError(t, err)

	renderValues, err := commonutil.ToRenderValues(
		loaded,
		merged,
		common.ReleaseOptions{Name: "pv-migrate-test", Namespace: "ns"},
		nil,
	)
	require.NoError(t, err)

	files, err := engine.Render(loaded, renderValues)
	require.NoError(t, err)

	var rcloneJob batchv1.Job
	require.NoError(t, yaml.Unmarshal([]byte(files["pv-migrate/templates/rclone/job.yaml"]), &rcloneJob))

	podSpec := &rcloneJob.Spec.Template.Spec
	require.NotNil(t, podSpec.SecurityContext)
	require.NotNil(t, podSpec.SecurityContext.FSGroup)
	assert.Equal(t, int64(nonRootUID), *podSpec.SecurityContext.FSGroup)

	require.Len(t, podSpec.Containers, 1)
	assert.Equal(t, "rclone", podSpec.Containers[0].Name)

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
