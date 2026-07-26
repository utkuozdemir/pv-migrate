// Package shell helps build the command strings that the rsync and rclone Jobs
// run through `sh -c`.
package shell

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Quote renders value as exactly one POSIX shell word.
//
// Single quotes are the only construct in which every byte except the single
// quote itself is literal, so a value is wrapped in them and each embedded
// single quote is closed, escaped and reopened.
func Quote(value string) string {
	if value == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// CheckSingleLine reports an error when value cannot appear in a command built by
// this package.
//
// Quoting makes such a character survive the shell, but not the step before it.
// The built command is interpolated into a YAML block scalar in the Job template,
// so a value carrying one does not produce a broken command, it produces a chart
// that will not render, and the user sees a parse error pointing at a template line
// instead of at their own flag. Rejecting it here keeps the failure next to its
// cause.
func CheckSingleLine(name, value string) error {
	if idx := strings.IndexFunc(value, notPlainYAML); idx >= 0 {
		found, _ := utf8.DecodeRuneInString(value[idx:])

		return fmt.Errorf("%s must not contain the character %q", name, found)
	}

	return nil
}

// notPlainYAML reports whether r is outside what YAML allows on one line of a
// block scalar. That is YAML's printable set minus its line breaks, so it covers
// more than a newline: the C0 and C1 control ranges, DEL, and the two Unicode
// separators, each of which was verified to make the rendered manifest unparseable
// in exactly the same way a newline does.
//
// Tab is the one control character YAML accepts there, the shell preserves it
// inside single quotes, and a file name may legitimately contain one, so it stays.
func notPlainYAML(char rune) bool {
	switch {
	case char == '\t':
		return false
	case char < 0x20: // C0 controls, including CR and LF
		return true
	case char >= 0x7f && char <= 0x9f: // DEL and the C1 controls, one of which YAML reads as a line break
		return true
	case char == '\u2028' || char == '\u2029': // line and paragraph separator
		return true
	default:
		return false
	}
}
