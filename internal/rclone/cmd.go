package rclone

import (
	"fmt"
	"strings"

	"github.com/utkuozdemir/pv-migrate/internal/shell"
)

const (
	DirectionBackup  = "backup"
	DirectionRestore = "restore"
)

const defaultProgressFlags = "--stats 1s --stats-log-level NOTICE --use-json-log --stats-one-line"

// Cmd holds the parameters for building an rclone command string.
type Cmd struct {
	Direction  string
	RemotePath string
	LocalPath  string
	ConfigPath string
	ExtraArgs  string
	Delete     bool
}

// Build produces the full rclone command string.
func (c *Cmd) Build() (string, error) {
	// The local path carries its flag name so the error points at what to change.
	// The other two are not flags: the remote path is assembled from --bucket,
	// --prefix and --name or from --remote, and the config path is where the
	// generated config is mounted in the pod.
	for _, field := range []struct {
		name  string
		value string
	}{
		{"--path", c.LocalPath},
		{"remote path", c.RemotePath},
		{"rclone config path", c.ConfigPath},
	} {
		if err := shell.CheckSingleLine(field.name, field.value); err != nil {
			return "", err
		}
	}

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

	action := "sync"
	if c.Direction == DirectionRestore && !c.Delete {
		action = "copy"
	}

	var builder strings.Builder

	fmt.Fprintf(&builder, "rclone %s", action)

	if c.ConfigPath != "" {
		fmt.Fprintf(&builder, " --config %s", shell.Quote(c.ConfigPath))
	}

	builder.WriteString(" " + defaultProgressFlags)

	fmt.Fprintf(&builder, " %s %s", shell.Quote(src), shell.Quote(dest))

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
