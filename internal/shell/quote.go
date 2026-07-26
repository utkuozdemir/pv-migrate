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
	// A path on Linux is any byte sequence other than NUL and the separator, so a
	// flag value can hold bytes that are not UTF-8 at all. YAML is defined over
	// characters rather than bytes and rejects those outright, so they fail the same
	// way a newline does. This is checked before the scan below, which decodes an
	// invalid byte to the replacement character and would let it through.
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", name)
	}

	if idx := strings.IndexFunc(value, notPlainYAML); idx >= 0 {
		found, _ := utf8.DecodeRuneInString(value[idx:])

		return fmt.Errorf("%s must not contain the character %q", name, found)
	}

	return nil
}

// notPlainYAML reports whether char is outside what the YAML reader Helm uses will
// carry on one line of a block scalar, which is its printable set minus the
// characters it breaks lines on. That is a good deal more than a newline: the C0 and
// C1 control ranges, DEL, the two Unicode separators, and the last two
// non-characters of the basic plane. Each was checked to make the rendered manifest
// unparseable in the same way a newline does.
//
// The rule is the reader's rather than the current specification's. YAML 1.2 breaks
// lines only on carriage return and newline, while this reader also breaks on the
// next-line control and the two separators, which is the older behaviour. Its
// printable set is the same in both, so the non-characters are outside either.
// Either way it is the reader that decides whether the chart renders.
//
// Tab is the one control character it accepts here, the shell preserves it inside
// single quotes, and a file name may legitimately contain one, so it stays.
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
	case char == '\ufffe' || char == '\uffff': // the two non-characters YAML's printable set stops short of
		return true
	default:
		return false
	}
}
