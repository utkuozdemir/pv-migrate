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
	SrcUseSsh   bool
	SrcSshUser  string
	SrcSshHost  string
	SrcPath     string
	DestUseSsh  bool
	DestSshUser string
	DestSshHost string
	DestPath    string
}

func (c *Cmd) Build() (string, error) {
	if c.SrcUseSsh && c.DestUseSsh {
		return "", fmt.Errorf("cannot use ssh on both source and destination")
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

	rsyncArgs := []string{"-azv", "--info=progress2,misc0,flist0",
		"--no-inc-recursive", "-e", sshArgsStr}
	if c.NoChown {
		rsyncArgs = append(rsyncArgs, "--no-o", "--no-g")
	}
	if c.Delete {
		rsyncArgs = append(rsyncArgs, "--delete")
	}

	rsyncArgsStr := strings.Join(rsyncArgs, " ")

	var src strings.Builder
	if c.SrcUseSsh {
		sshDestUser := "root"
		if c.SrcSshUser != "" {
			sshDestUser = c.SrcSshUser
		}
		src.WriteString(fmt.Sprintf("%s@%s:", sshDestUser, c.SrcSshHost))
	}
	src.WriteString(c.SrcPath)

	var dest strings.Builder
	if c.DestUseSsh {
		sshDestUser := "root"
		if c.DestSshUser != "" {
			sshDestUser = c.DestSshUser
		}
		dest.WriteString(fmt.Sprintf("%s@%s:", sshDestUser, c.DestSshHost))
	}
	dest.WriteString(c.DestPath)

	return fmt.Sprintf("%s %s %s %s", cmd, rsyncArgsStr, src.String(), dest.String()), nil
}
