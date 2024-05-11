package rsync

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Cmd struct {
	Port        int
	NoChown     bool
	Delete      bool
	SrcUseSSH   bool
	DestUseSSH  bool
	Command     string
	SrcSSHUser  string
	SrcSSHHost  string
	SrcPath     string
	DestSSHUser string
	DestSSHHost string
	DestPath    string
	Compress    bool
}

func (c *Cmd) Build() (string, error) {
	if c.SrcUseSSH && c.DestUseSSH {
		return "", errors.New("cannot use ssh on both source and destination")
	}

	cmd := "rsync"
	if c.Command != "" {
		cmd = c.Command
	}

	sshArgs := []string{
		"ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
	}
	if c.Port != 0 {
		sshArgs = append(sshArgs, "-p", strconv.Itoa(c.Port))
	}

	sshArgsStr := fmt.Sprintf("\"%s\"", strings.Join(sshArgs, " "))

	rsyncArgs := []string{
		"-av", "--info=progress2,misc0,flist0",
		"--no-inc-recursive", "-e", sshArgsStr,
	}

	if c.Compress {
		rsyncArgs = append(rsyncArgs, "-z")
	}

	if c.NoChown {
		rsyncArgs = append(rsyncArgs, "--no-o", "--no-g")
	}

	if c.Delete {
		rsyncArgs = append(rsyncArgs, "--delete")
	}

	rsyncArgsStr := strings.Join(rsyncArgs, " ")

	src := c.buildSrc()
	dest := c.buildDest()

	return fmt.Sprintf("%s %s %s %s", cmd, rsyncArgsStr, src, dest), nil
}

func (c *Cmd) buildSrc() string {
	var src strings.Builder

	if c.SrcUseSSH {
		sshDestUser := "root"
		if c.SrcSSHUser != "" {
			sshDestUser = c.SrcSSHUser
		}

		src.WriteString(fmt.Sprintf("%s@%s:", sshDestUser, c.SrcSSHHost))
	}

	src.WriteString(c.SrcPath)

	return src.String()
}

func (c *Cmd) buildDest() string {
	var dest strings.Builder

	if c.DestUseSSH {
		sshDestUser := "root"
		if c.DestSSHUser != "" {
			sshDestUser = c.DestSSHUser
		}

		dest.WriteString(fmt.Sprintf("%s@%s:", sshDestUser, c.DestSSHHost))
	}

	dest.WriteString(c.DestPath)

	return dest.String()
}
