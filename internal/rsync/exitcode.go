package rsync

// exitCodeMeanings are rsync's exit values as its manual page words them. They
// are quoted rather than paraphrased on purpose: an exit code is an observed
// fact, its documented meaning is another one, and the scenario that produced it
// is neither. Code 23 alone covers unreadable sources, unwritable destinations
// and broken symlinks, so naming any of them would be a guess.
var exitCodeMeanings = map[int]string{
	1:  "Syntax or usage error",
	2:  "Protocol incompatibility",
	3:  "Errors selecting input/output files, dirs",
	4:  "Requested action not supported",
	5:  "Error starting client-server protocol",
	6:  "Daemon unable to append to log-file",
	10: "Error in socket I/O",
	11: "Error in file I/O",
	12: "Error in rsync protocol data stream",
	13: "Errors with program diagnostics",
	14: "Error in IPC code",
	20: "Received SIGUSR1 or SIGINT",
	21: "Some error returned by waitpid()",
	22: "Error allocating core memory buffers",
	23: "Partial transfer due to error",
	24: "Partial transfer due to vanished source files",
	25: "The --max-delete limit stopped deletions",
	30: "Timeout in data send/receive",
	35: "Timeout waiting for daemon connection",
}

// VanishedFilesExitCode is what rsync exits with when files disappeared from the
// source while it was reading them. Everything it did transfer is intact.
const VanishedFilesExitCode = 24

// VanishedFilesMarker is the start of the line the chart's rsync job script
// prints when it treats a vanished-files exit as a success. The client scans
// the job log for it, and the script render test asserts the script still
// prints it, which is what keeps the two spellings from drifting apart.
const VanishedFilesMarker = "pv-migrate: some source files vanished during the transfer"

// remoteShellExitCode is not an rsync exit value. rsync passes the remote
// shell's status through, and ssh uses this one for its own failures.
const remoteShellExitCode = 255

// Interpret returns what rsync's documentation says about an exit status, or an
// empty string when the status is not one it documents.
func Interpret(code int) string {
	if code == remoteShellExitCode {
		return "255 is not an rsync exit value: rsync passes through the status of the remote shell, " +
			"and ssh uses 255 for its own errors"
	}

	meaning, ok := exitCodeMeanings[code]
	if !ok {
		return ""
	}

	return "rsync documents this exit code as: " + meaning
}
