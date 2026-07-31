// Package console colors the plain-text report blocks semantically. The
// coloring is driven by what the code knows about a line at render time, a
// failed phase or a warning event or a healthy replica count, never by
// matching words in the text, so a pod named "failed-app" cannot trick it.
package console

import "os"

// Palette renders semantic colors. The zero value renders plain text, which is
// what non-terminal writers and structured log streams use, and what keeps
// every existing plain-text assertion valid.
type Palette struct {
	Enabled bool
}

const reset = "\x1b[0m"

// Failure marks the headline of a failure report. Bold red.
func (p Palette) Failure(s string) string { return p.wrap("1;31", s) }

// Bad marks a hard failure fact: a failed phase, a non-zero exit, a rejected
// image pull. Red.
func (p Palette) Bad(s string) string { return p.wrap("31", s) }

// Warn marks something abnormal but not terminal: a warning event, a pending
// phase, a declined strategy. Yellow.
func (p Palette) Warn(s string) string { return p.wrap("33", s) }

// Good marks a healthy fact, which by contrast makes the one abnormal line in
// a block stand out. Green.
func (p Palette) Good(s string) string { return p.wrap("32", s) }

// Dim marks neutral framing and inventory: block headings' object names, the
// tail's caption, facts that are neither good nor bad. Faint.
func (p Palette) Dim(s string) string { return p.wrap("2", s) }

// Bold marks a section heading.
func (p Palette) Bold(s string) string { return p.wrap("1", s) }

func (p Palette) wrap(code, s string) string {
	if !p.Enabled || s == "" {
		return s
	}

	return "\x1b[" + code + "m" + s + reset
}

// ForTerminal reports whether colored output makes sense: a terminal on the
// other end, no machine-readable stream sharing it, and no NO_COLOR ask.
func ForTerminal(isTerminal, structuredLogs bool) bool {
	return isTerminal && !structuredLogs && os.Getenv("NO_COLOR") == ""
}
