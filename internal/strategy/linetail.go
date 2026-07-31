package strategy

import (
	"bytes"
	"slices"
	"strings"
	"sync"
)

// sessionTailLines bounds the raw lines kept from a local rsync session, which
// unlike an in-cluster job has no log to fetch back after a failure.
const sessionTailLines = 20

// lineTail is a writer that keeps the last raw lines written through it. It
// splits on both line feeds and the carriage returns rsync uses to redraw its
// progress line, and it holds an unterminated final line too, since a process
// that dies mid-line dies on the line worth reading.
type lineTail struct {
	mu      sync.Mutex
	limit   int
	partial []byte
	lines   []string
}

func (t *lineTail) Write(data []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.partial = append(t.partial, data...)

	for {
		idx := bytes.IndexAny(t.partial, "\r\n")
		if idx < 0 {
			break
		}

		line := strings.TrimSpace(string(t.partial[:idx]))
		t.partial = t.partial[idx+1:]

		if line != "" {
			t.record(line)
		}
	}

	return len(data), nil
}

// Lines returns the recorded lines, oldest first, including a trailing
// unterminated one.
func (t *lineTail) Lines() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	lines := slices.Clone(t.lines)

	if rest := strings.TrimSpace(string(t.partial)); rest != "" {
		lines = append(lines, rest)
	}

	return lines
}

func (t *lineTail) record(line string) {
	if len(t.lines) == t.limit {
		copy(t.lines, t.lines[1:])
		t.lines = t.lines[:t.limit-1]
	}

	t.lines = append(t.lines, line)
}
