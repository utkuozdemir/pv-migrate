package rclone

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/utkuozdemir/pv-migrate/internal/posixmeta"
	"github.com/utkuozdemir/pv-migrate/internal/shell"
)

const (
	DirectionBackup  = "backup"
	DirectionRestore = "restore"

	// PosixManifestFileName is the compressed POSIX metadata file stored at the root of each backup.
	PosixManifestFileName = posixmeta.ManifestFileName

	// MissingPosixMetadataMarker is printed by the job script when restoring a backup without a POSIX manifest.
	MissingPosixMetadataMarker = "pv-migrate: posix metadata not found, permissions and ownership were not restored"
)

const defaultProgressFlags = "--stats 1s --stats-log-level NOTICE --use-json-log --stats-one-line"

// shortFlagCluster matches a run of rclone short flags, which are letters only.
// Requiring the whole token to be letters keeps a value like -1n from reading as
// a boolean shorthand.
var shortFlagCluster = regexp.MustCompile(`^-[a-zA-Z]+$`)

// Cmd holds the parameters for building an rclone command string.
type Cmd struct {
	Direction        string
	RemotePath       string
	LocalPath        string
	ConfigPath       string
	ExtraArgs        string
	Delete           bool
	ExcludeLostFound bool
}

// Build produces the full rclone command string.
func (c *Cmd) Build() (string, error) {
	if err := c.checkFields(); err != nil {
		return "", err
	}

	src, dest, action, err := c.resolveTransfer()
	if err != nil {
		return "", err
	}

	var builder strings.Builder

	fmt.Fprintf(&builder, "rclone %s", action)

	if c.ConfigPath != "" {
		fmt.Fprintf(&builder, " --config %s", shell.Quote(c.ConfigPath))
	}

	c.writeFilterFlags(&builder)
	builder.WriteString(" " + defaultProgressFlags)

	fmt.Fprintf(&builder, " %s %s", shell.Quote(src), shell.Quote(dest))

	if c.ExtraArgs != "" {
		fmt.Fprintf(&builder, " %s", c.ExtraArgs)
	}

	return builder.String(), nil
}

func (c *Cmd) writeFilterFlags(builder *strings.Builder) {
	// rclone refuses --links when --copy-links (-L) is also requested.
	if !HasBoolFlag(c.ExtraArgs, "--copy-links", 'L') {
		builder.WriteString(" --links")
	}

	builder.WriteString(" --exclude /" + PosixManifestFileName)

	if c.ExcludeLostFound {
		builder.WriteString(" --exclude " + shell.Quote("/lost+found/**"))
	}
}

func (c *Cmd) checkFields() error {
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
			return err
		}
	}

	return nil
}

func (c *Cmd) resolveTransfer() (string, string, string, error) {
	var src, dest string

	switch c.Direction {
	case DirectionBackup:
		src = c.LocalPath
		dest = c.RemotePath
	case DirectionRestore:
		src = c.RemotePath
		dest = c.LocalPath
	default:
		return "", "", "", fmt.Errorf(
			"invalid direction: %q, must be %q or %q",
			c.Direction,
			DirectionBackup,
			DirectionRestore,
		)
	}

	action := "sync"
	if c.Direction == DirectionRestore && !c.Delete {
		action = "copy"
	}

	return src, dest, action, nil
}

// HasBoolFlag reports whether the raw argument string sets a pflag boolean flag.
// pflag applies each occurrence left to right, so the last occurrence decides.
func HasBoolFlag(extraArgs, longName string, shortLetter rune) bool {
	enabled := false

	for arg := range strings.FieldsSeq(extraArgs) {
		name, value, assigned := strings.Cut(arg, "=")

		if assigned {
			applyAssignedFlag(&enabled, name, value, longName, shortLetter)

			continue
		}

		// rclone uses pflag, so short flags may be bundled together (for example -nv).
		if arg == longName || (shortFlagCluster.MatchString(arg) && strings.ContainsRune(arg, shortLetter)) {
			enabled = true
		}
	}

	return enabled
}

func applyAssignedFlag(enabled *bool, name, value, longName string, shortLetter rune) {
	switch {
	case name == longName:
		setFromBool(enabled, value)
	case shortFlagCluster.MatchString(name):
		// pflag binds an assigned value only to the last shorthand in a bundle.
		letters := strings.TrimPrefix(name, "-")

		for idx, letter := range letters {
			switch {
			case letter != shortLetter:
			case idx == len(letters)-1:
				setFromBool(enabled, value)
			default:
				*enabled = true
			}
		}
	}
}

func setFromBool(target *bool, value string) {
	if enabled, err := strconv.ParseBool(value); err == nil {
		*target = enabled
	}
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
// remote:<bucket>/<prefix>/<name>.meta.yaml.
func BuildMetadataRemotePath(bucket, prefix, name string) string {
	if prefix == "" {
		return fmt.Sprintf("%s:%s/%s.meta.yaml", remoteName, bucket, name)
	}

	return fmt.Sprintf("%s:%s/%s/%s.meta.yaml", remoteName, bucket, prefix, name)
}

// BuildPosixMetadataRemotePath constructs the remote path for the POSIX manifest inside the backup root.
func BuildPosixMetadataRemotePath(remotePath string) string {
	if strings.HasSuffix(remotePath, "/") || strings.HasSuffix(remotePath, ":") {
		return remotePath + PosixManifestFileName
	}

	return remotePath + "/" + PosixManifestFileName
}

// BuildRemotePathRaw returns the user-provided remote spec as-is (for --rclone-config mode).
func BuildRemotePathRaw(remote string) string {
	return remote
}
