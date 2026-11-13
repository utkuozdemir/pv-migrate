package opid_test

import (
	"io"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	"helm.sh/helm/v4/pkg/release"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	rspb "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"

	"github.com/utkuozdemir/pv-migrate/internal/opid"
)

func TestReleaseFilter(t *testing.T) {
	t.Parallel()

	filter := regexp.MustCompile(opid.ReleaseFilter("app"))

	matching := []string{
		"pv-migrate-app-mount",
		"pv-migrate-app-clusterip",
		"pv-migrate-app-loadbalancer-src",
		"pv-migrate-app-loadbalancer-dest",
		"pv-migrate-app-nodeport-src",
		"pv-migrate-app-local-dest",
		"pv-migrate-app-backup",
		"pv-migrate-app-restore",
	}
	for _, name := range matching {
		assert.True(t, filter.MatchString(name), name)
	}

	nonMatching := []string{
		"pv-migrate-app-db-mount",
		"pv-migrate-app-db-backup",
		"pv-migrate-app-src-mount",
		"pv-migrate-app-dest-loadbalancer-src",
		"pv-migrate-app-mount-loadbalancer-src",
		"pv-migrate-app-mount-mount",
		"pv-migrate-app-backup-backup",
		"pv-migrate-app2-mount",
		"pv-migrate-app-",
		"other-app-mount",
	}
	for _, name := range nonMatching {
		assert.False(t, filter.MatchString(name), name)
	}
}

func TestMatchesJob(t *testing.T) {
	t.Parallel()

	assert.True(t, opid.MatchesJob("pv-migrate-app-mount-rsync", "app"))
	assert.True(t, opid.MatchesJob("pv-migrate-app-loadbalancer-dest-rsync", "app"))
	assert.True(t, opid.MatchesJob("pv-migrate-app-backup-rclone", "app"))
	assert.True(t, opid.MatchesJob("pv-migrate-app-restore-rclone", "app"))

	assert.False(t, opid.MatchesJob("pv-migrate-app-db-backup-rclone", "app"))
	assert.False(t, opid.MatchesJob("pv-migrate-app-src-mount-rsync", "app"))
	assert.False(t, opid.MatchesJob("pv-migrate-app-dest-loadbalancer-src-rsync", "app"))
	assert.False(t, opid.MatchesJob("pv-migrate-app-clusterip-clusterip-rsync", "app"))
	assert.False(t, opid.MatchesJob("pv-migrate-app-clusterip-sshd", "app"))
}

func TestReleaseFilterWithHelmList(t *testing.T) {
	t.Parallel()

	releases := storage.Init(driver.NewMemory())
	for _, name := range []string{
		"pv-migrate-app-clusterip-src",
		"pv-migrate-app-clusterip-dest",
		"pv-migrate-app-db-clusterip",
		"pv-migrate-app-backup-backup",
	} {
		require.NoError(t, releases.Create(&rspb.Release{
			Name:      name,
			Namespace: "default",
			Version:   1,
			Info:      &rspb.Info{Status: releasecommon.StatusDeployed},
		}))
	}

	list := action.NewList(&action.Configuration{
		Releases:     releases,
		KubeClient:   &kubefake.PrintingKubeClient{Out: io.Discard},
		Capabilities: common.DefaultCapabilities,
	})
	list.Filter = opid.ReleaseFilter("app")
	list.StateMask = action.ListAll

	matched, err := list.Run()
	require.NoError(t, err)
	require.Len(t, matched, 2)

	got := make([]string, 0, len(matched))
	for _, rel := range matched {
		acc, accErr := release.NewAccessor(rel)
		require.NoError(t, accErr)

		got = append(got, acc.Name())
	}

	assert.ElementsMatch(t, []string{
		"pv-migrate-app-clusterip-src",
		"pv-migrate-app-clusterip-dest",
	}, got)
}
