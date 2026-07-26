package rsync

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/utkuozdemir/pv-migrate/internal/shell"
)

type Cmd struct {
	Port        int
	NoChown     bool
	NonRoot     bool
	Delete      bool
	SrcUseSSH   bool
	DestUseSSH  bool
	SrcSSHUser  string
	SrcSSHHost  string
	SrcPath     string
	DestSSHUser string
	DestSSHHost string
	DestPath    string
	Compress    bool
	ExtraArgs   string
}

func (c *Cmd) Build() (string, error) {
	if c.SrcUseSSH && c.DestUseSSH {
		return "", errors.New("cannot use ssh on both source and destination")
	}

	if err := c.validate(); err != nil {
		return "", err
	}

	// The source and destination specs are quoted as single shell words: the
	// paths come from --source-path/--dest-path and the hosts can come from
	// --dest-host-override, so either may legitimately contain a space. ExtraArgs
	// is left alone on purpose, since it is documented as raw rsync flags.
	return fmt.Sprintf("rsync %s %s %s",
		strings.Join(c.args(), " "),
		shell.Quote(c.buildSrc()),
		shell.Quote(c.buildDest()),
	), nil
}

// args returns rsync's own flags, including the -e value that tells it how to
// reach the remote side.
func (c *Cmd) args() []string {
	args := []string{
		"-av", "--info=progress2,misc0,flist0",
		"--no-inc-recursive", "-e", shell.Quote(strings.Join(c.sshArgs(), " ")),
	}

	if c.Compress {
		args = append(args, "-z")
	}

	if c.NoChown || c.NonRoot {
		args = append(args, "--no-o", "--no-g")
	}

	if c.NonRoot {
		args = append(args, "--omit-dir-times")
	}

	if c.Delete {
		args = append(args, "--delete")
	}

	if c.ExtraArgs != "" {
		args = append(args, c.ExtraArgs)
	}

	return args
}

func (c *Cmd) sshArgs() []string {
	args := []string{
		"ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		// ServerAliveInterval/CountMax prevent intermediate load balancers and proxies
		// from dropping idle SSH connections during long file-list-building phases.
		"-o", "ServerAliveInterval=10",
		"-o", "ServerAliveCountMax=3",
	}

	if c.Port != 0 {
		args = append(args, "-p", strconv.Itoa(c.Port))
	}

	return args
}

// validate rejects values that cannot be represented in the built command. The
// paths carry their flag names so the error points at what to change; the ssh
// host may instead have been resolved from the cluster, so it is named for what
// it is.
func (c *Cmd) validate() error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{"--source-path", c.SrcPath},
		{"--dest-path", c.DestPath},
		{"ssh host", c.SrcSSHHost},
		{"ssh host", c.DestSSHHost},
	} {
		if err := shell.CheckSingleLine(field.name, field.value); err != nil {
			return err
		}
	}

	return nil
}

// buildSrc returns the rsync source spec, either a bare path or a
// user@host:path remote spec.
func (c *Cmd) buildSrc() string {
	if !c.SrcUseSSH {
		return c.SrcPath
	}

	return sshUser(c.SrcSSHUser) + "@" + c.SrcSSHHost + ":" + c.SrcPath
}

// buildDest returns the rsync destination spec, either a bare path or a
// user@host:path remote spec.
func (c *Cmd) buildDest() string {
	if !c.DestUseSSH {
		return c.DestPath
	}

	return sshUser(c.DestSSHUser) + "@" + c.DestSSHHost + ":" + c.DestPath
}

func sshUser(user string) string {
	if user == "" {
		return "root"
	}

	return user
}
