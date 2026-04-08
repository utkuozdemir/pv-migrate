package rclone

import (
	"fmt"
	"strings"
)

const (
	DirectionBackup  = "backup"
	DirectionRestore = "restore"
)

// Cmd holds the parameters for building an rclone command string.
type Cmd struct {
	Direction  string
	RemotePath string
	LocalPath  string
	ConfigPath string
	ExtraArgs  string
}

// Build produces the full rclone command string.
func (c *Cmd) Build() (string, error) {
	var src, dest string

	switch c.Direction {
	case DirectionBackup:
		src = c.LocalPath
		dest = c.RemotePath
	case DirectionRestore:
		src = c.RemotePath
		dest = c.LocalPath
	default:
		return "", fmt.Errorf("invalid direction: %q, must be %q or %q", c.Direction, DirectionBackup, DirectionRestore)
	}

	var builder strings.Builder

	builder.WriteString("rclone sync")

	if c.ConfigPath != "" {
		fmt.Fprintf(&builder, " --config %s", c.ConfigPath)
	}

	fmt.Fprintf(&builder, " %s %s", src, dest)

	if c.ExtraArgs != "" {
		fmt.Fprintf(&builder, " %s", c.ExtraArgs)
	}

	return builder.String(), nil
}

// BuildRemotePath constructs the remote path for backup data:
// remote:<bucket>/<prefix>/<name>/
// If prefix is empty, the prefix segment is omitted.
func BuildRemotePath(bucket, prefix, name string) string {
	if prefix == "" {
		return fmt.Sprintf("%s:%s/%s/", remoteName, bucket, name)
	}

	return fmt.Sprintf("%s:%s/%s/%s/", remoteName, bucket, prefix, name)
}

// BuildMetadataRemotePath constructs the remote path for the metadata sidecar file:
// remote:<bucket>/<prefix>/<name>.meta.yaml
func BuildMetadataRemotePath(bucket, prefix, name string) string {
	if prefix == "" {
		return fmt.Sprintf("%s:%s/%s.meta.yaml", remoteName, bucket, name)
	}

	return fmt.Sprintf("%s:%s/%s/%s.meta.yaml", remoteName, bucket, prefix, name)
}

// BuildRemotePathRaw returns the user-provided remote spec as-is (for --rclone-config mode).
func BuildRemotePathRaw(remote string) string {
	return remote
}
