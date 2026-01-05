package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReleaseImageTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version  string
		expected string
	}{
		// GoReleaser sets version without "v" prefix
		{"2.3.0", "v2.3.0"},
		{"1.0.0", "v1.0.0"},
		{"0.1.0", "v0.1.0"},

		// Pre-releases should get image tags
		{"2.3.0-rc.1", "v2.3.0-rc.1"},
		{"2.3.0-alpha.1", "v2.3.0-alpha.1"},
		{"2.3.0-beta.1", "v2.3.0-beta.1"},

		// Snapshots should not
		{"2.2.1-SNAPSHOT-43a0f03", ""},

		// Local builds should not
		{"dev", ""},

		// Edge cases
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, releaseImageTag(tt.version))
		})
	}
}
