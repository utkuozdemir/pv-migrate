package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTransfersFile(t *testing.T) {
	t.Parallel()

	content := `- source: pvc-a
  dest: new-pvc-a
- source: pvc-b
  dest: new-pvc-b
  destNamespace: other-ns
  sourcePath: /data
  destPath: /backup
`

	tmpFile := filepath.Join(t.TempDir(), "transfers.yaml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(content), 0o644))

	transfers, err := parseTransfersFile(tmpFile)
	require.NoError(t, err)
	require.Len(t, transfers, 2)

	assert.Equal(t, "pvc-a", transfers[0].Source.Name)
	assert.Equal(t, "new-pvc-a", transfers[0].Dest.Name)
	assert.Empty(t, transfers[0].Source.Namespace)
	assert.Empty(t, transfers[0].Dest.Namespace)

	assert.Equal(t, "pvc-b", transfers[1].Source.Name)
	assert.Equal(t, "new-pvc-b", transfers[1].Dest.Name)
	assert.Equal(t, "other-ns", transfers[1].Dest.Namespace)
	assert.Equal(t, "/data", transfers[1].Source.Path)
	assert.Equal(t, "/backup", transfers[1].Dest.Path)
}

func TestParseTransfersFileEmpty(t *testing.T) {
	t.Parallel()

	tmpFile := filepath.Join(t.TempDir(), "empty.yaml")
	require.NoError(t, os.WriteFile(tmpFile, []byte("[]"), 0o644))

	_, err := parseTransfersFile(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestParseTransfersFileMissingSource(t *testing.T) {
	t.Parallel()

	content := `- dest: new-pvc-a
`

	tmpFile := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(content), 0o644))

	_, err := parseTransfersFile(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source is required")
}

func TestParseTransfersFileMissingDest(t *testing.T) {
	t.Parallel()

	content := `- source: pvc-a
`

	tmpFile := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(content), 0o644))

	_, err := parseTransfersFile(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dest is required")
}

func TestParseTransfersFileNotFound(t *testing.T) {
	t.Parallel()

	_, err := parseTransfersFile("/nonexistent/transfers.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read")
}
