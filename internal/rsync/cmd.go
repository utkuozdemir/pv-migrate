package rsync

import (
	"fmt"
	"strconv"
	"strings"
)

type Cmd struct {
	Command     string
	Port        int
	NoChown     bool
	Delete      bool
	SrcPath     string
	DestPath    string
	UseSshDest  bool
	SshDestUser string
	SshDestHost string
}

func (c *Cmd) Build() string {
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

	rsyncArgs := []string{"-azv", "--info=progress2,misc0,flist0",
		"--no-inc-recursive", "-e", sshArgsStr}
	if c.NoChown {
		rsyncArgs = append(rsyncArgs, "--no-o", "--no-g")
	}
	if c.Delete {
		rsyncArgs = append(rsyncArgs, "--delete")
	}

	rsyncArgsStr := strings.Join(rsyncArgs, " ")

	var dest strings.Builder
	if c.UseSshDest {
		dest.WriteString(fmt.Sprintf("%s@%s:", c.SshDestUser, c.SshDestHost))
	}
	dest.WriteString(c.DestPath)

	return fmt.Sprintf("%s %s %s %s", cmd, rsyncArgsStr, c.SrcPath, dest.String())
}
