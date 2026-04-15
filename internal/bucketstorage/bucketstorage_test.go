package bucketstorage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen
func TestBuildRemotePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      Request
		expected string
		wantErr  string
	}{
		{
			name: "raw config mode bypasses name and bucket requirements",
			req: Request{
				RcloneConfigFile: "/tmp/rclone.conf",
				Remote:           "manual:bucket/path",
			},
			expected: "manual:bucket/path",
		},
		{
			name: "raw config mode requires remote",
			req: Request{
				RcloneConfigFile: "/tmp/rclone.conf",
			},
			wantErr: "--remote is required when using --rclone-config",
		},
		{
			name: "standard mode requires bucket",
			req: Request{
				Name: "backup",
			},
			wantErr: "--bucket is required",
		},
		{
			name: "standard mode requires name",
			req: Request{
				Bucket: "bucket",
			},
			wantErr: "--name is required",
		},
		{
			name: "allows slash separated prefix",
			req: Request{
				Bucket: "bucket",
				Prefix: "team-a/backups",
				Name:   "backup",
			},
			expected: "remote:bucket/team-a/backups/backup/",
		},
		{
			name: "rejects invalid name",
			req: Request{
				Bucket: "bucket",
				Name:   "bad/name",
			},
			wantErr: `--name "bad/name" contains invalid characters`,
		},
		{
			name: "rejects empty prefix segment",
			req: Request{
				Bucket: "bucket",
				Prefix: "team-a//backups",
				Name:   "backup",
			},
			wantErr: `must not have leading/trailing '/' or empty path segments`,
		},
		{
			name: "rejects invalid prefix segment characters",
			req: Request{
				Bucket: "bucket",
				Prefix: "team-a/bad prefix",
				Name:   "backup",
			},
			wantErr: `--prefix "team-a/bad prefix" contains invalid characters`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := buildRemotePath(&tt.req)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
