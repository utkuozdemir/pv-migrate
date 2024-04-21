package helm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/helm"
)

func TestLoadChart(t *testing.T) {
	t.Parallel()

	chart, err := helm.LoadChart()
	require.NoError(t, err)

	assert.Equal(t, "pv-migrate", chart.Metadata.Name)
	assert.NotEmpty(t, chart.Metadata.Version, "chart version should not be empty")
	assert.NotEmpty(t, chart.Values, "chart values should not be empty")
	assert.NotEmpty(t, chart.Templates, "chart templates should not be empty")
}
