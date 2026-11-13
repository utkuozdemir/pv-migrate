package migrator_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/migrator"
)

func TestPrintDetachMessage_StructuredLogsKeepsJSONStreamClean(t *testing.T) {
	t.Parallel()

	var structuredOut bytes.Buffer

	migrator.PrintDetachMessage(
		&migration.Request{Writer: &structuredOut, StructuredLogs: true},
		"op-123",
		"clusterip",
		slog.New(slog.NewJSONHandler(&structuredOut, nil)),
	)

	lines := strings.Split(strings.TrimRight(structuredOut.String(), "\n"), "\n")
	require.Len(t, lines, 1)

	var record map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &record))
	assert.Equal(t, "clusterip", record["strategy"])
	assert.Equal(t, "op-123", record["id"])

	var textOut bytes.Buffer

	migrator.PrintDetachMessage(
		&migration.Request{Writer: &textOut, StructuredLogs: false},
		"op-123",
		"clusterip",
		slog.New(slog.DiscardHandler),
	)
	assert.Contains(t, textOut.String(), "pv-migrate status op-123")
	assert.Contains(t, textOut.String(), "pv-migrate cleanup op-123")
}
