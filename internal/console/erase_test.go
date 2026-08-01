package console_test

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/lmittmann/tint"
	"github.com/schollz/progressbar/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/console"
)

// TestRecordDoesNotLandOnTheProgressBar is issue 449 as a test. It paints a real
// bar, logs a real record the way the CLI does, and paints again. The transcript
// then goes through a screen, which answers the only question that matters: what
// a person would have seen.
func TestRecordDoesNotLandOnTheProgressBar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		eraseFirst bool
		wantClean  bool
	}{
		{name: "records erase the bar's line", eraseFirst: true, wantClean: true},
		{name: "without erasing, they collide", eraseFirst: false, wantClean: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var transcript bytes.Buffer

			bar := progressbar.NewOptions64(100,
				progressbar.OptionSetWriter(&transcript),
				progressbar.OptionUseANSICodes(true),
				progressbar.OptionFullWidth(),
				progressbar.OptionSetDescription("Copying data..."))

			var records io.Writer = &transcript
			if tt.eraseFirst {
				records = console.EraseLineBefore(&transcript)
			}

			logger := slog.New(tint.NewTextHandler(records, &tint.Options{NoColor: true}))

			require.NoError(t, bar.Set64(8))
			logger.Warn("Pod is not starting yet")
			require.NoError(t, bar.Set64(9))

			rendered := render(transcript.String())
			screen := strings.Join(rendered, "\n")

			recordLine, found := lineContaining(rendered, "Pod is not starting yet")
			require.True(t, found, "the record must survive at all:\n%s", screen)

			assert.Equal(t, tt.wantClean, !strings.Contains(recordLine, "Copying data"),
				"record line was %q in:\n%s", recordLine, screen)

			if !tt.wantClean {
				return
			}

			assert.Equal(t, 1, countLinesContaining(rendered, "Copying data"),
				"one bar, not a stranded copy:\n%s", screen)
			assert.NotContains(t, recordLine, "|", "no tail of the bar is left beside the record")
		})
	}
}

// TestEachRecordIsOneWrite pins what the fix rests on: a record and its erase
// reach the terminal together, so a bar frame cannot land between them.
func TestEachRecordIsOneWrite(t *testing.T) {
	t.Parallel()

	counter := &countingWriter{}
	logger := slog.New(tint.NewTextHandler(console.EraseLineBefore(counter), &tint.Options{NoColor: true}))

	logger.Info("one")
	logger.Info("two", "with", strings.Repeat("a long attribute value ", 200))

	assert.Equal(t, 2, counter.writes, "one write per record, whatever its length")

	for _, written := range counter.parts {
		assert.True(t, strings.HasPrefix(written, "\r\x1b[2K"), "each write erases first: %q", written)
	}
}

func TestEraseLineBeforeCountsOnlyItsCallersBytes(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	payload := []byte("a record\n")

	written, err := console.EraseLineBefore(&buf).Write(payload)
	require.NoError(t, err)
	assert.Equal(t, len(payload), written, "a writer reports the bytes it was given")
	assert.True(t, strings.HasSuffix(buf.String(), string(payload)))
	assert.Greater(t, buf.Len(), len(payload), "the erase sequence went out with it")
}

func TestEraseLineBeforeReportsAShortWrite(t *testing.T) {
	t.Parallel()

	written, err := console.EraseLineBefore(shortWriter{}).Write([]byte("a record\n"))
	require.ErrorIs(t, err, io.ErrShortWrite)
	assert.Zero(t, written)
}

type countingWriter struct {
	writes int
	parts  []string
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.writes++
	c.parts = append(c.parts, string(p))

	return len(p), nil
}

// shortWriter accepts the erase sequence and nothing else, without saying so.
type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	return min(len(p), len("\r\x1b[2K")), nil
}

// render turns a transcript into the lines a terminal would be showing. It
// implements only what these tests write: a carriage return returns to the start
// of the line, a newline starts a new one, CSI K erases to the end of the line,
// and any other escape sequence is styling that occupies no columns.
func render(transcript string) []string {
	lines := [][]rune{{}}
	row, column := 0, 0

	runes := []rune(transcript)

	for at := 0; at < len(runes); at++ {
		switch {
		case runes[at] == '\r':
			column = 0
		case runes[at] == '\n':
			lines = append(lines, []rune{})
			row++
			column = 0
		case isEscape(runes, at):
			at = applyEscape(runes, at, lines[row], column)
		default:
			lines[row] = place(lines[row], column, runes[at])
			column++
		}
	}

	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		rendered = append(rendered, strings.TrimRight(string(line), " "))
	}

	return rendered
}

func isEscape(runes []rune, at int) bool {
	return runes[at] == 0x1b && at+1 < len(runes) && runes[at+1] == '['
}

// applyEscape handles the one sequence these tests care about, erasing the line,
// and returns where the sequence ended.
func applyEscape(runes []rune, at int, line []rune, column int) int {
	end := at + 2
	for end < len(runes) && !isFinalByte(runes[end]) {
		end++
	}

	if end < len(runes) && runes[end] == 'K' {
		eraseToEnd(line, string(runes[at+2:end]), column)
	}

	return end
}

func isFinalByte(r rune) bool { return r >= '@' && r <= '~' }

func eraseToEnd(line []rune, params string, column int) {
	from := column
	if params == "2" { // the whole line, wherever the cursor is
		from = 0
	}

	for i := from; i < len(line); i++ {
		line[i] = ' '
	}
}

func place(line []rune, column int, r rune) []rune {
	for len(line) <= column {
		line = append(line, ' ')
	}

	line[column] = r

	return line
}

func lineContaining(lines []string, text string) (string, bool) {
	for _, line := range lines {
		if strings.Contains(line, text) {
			return line, true
		}
	}

	return "", false
}

func countLinesContaining(lines []string, text string) int {
	found := 0

	for _, line := range lines {
		if strings.Contains(line, text) {
			found++
		}
	}

	return found
}
