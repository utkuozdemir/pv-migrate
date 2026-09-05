// Package narrate renders the text log as a story: one step line per record,
// and the details of a step indented under it. It is a slog handler, so the
// same records reach the JSON handler unchanged, and a detail is an ordinary
// info record that carries its depth as an attribute.
//
// The shape: a step opens a paragraph, so a blank line goes before every step
// but the first, and the details of a step stay under it without one, three
// spaces of indent per level, two levels at most. Nothing in it depends on
// color, so a log file reads the same as a terminal. Blocks that other code
// writes to the same stream directly set themselves apart with a blank line
// before, and with one after when details follow them.
package narrate

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/utkuozdemir/pv-migrate/internal/console"
)

const (
	// IndentKey is the attribute a detail record carries: how deep it sits
	// under the step above it, 1 or 2. A record without it is a step.
	IndentKey = "indent"

	indentWidth = 3
	maxDepth    = 2
)

// Detail returns a logger whose records are drawn as details of the step
// logged before them, the given depth deeper than the logger they derive from,
// so a detail logger handed down and wrapped again goes one level further.
func Detail(logger *slog.Logger, depth int) *slog.Logger {
	return logger.With(slog.Int(IndentKey, depth))
}

// Options configures the handler.
type Options struct {
	// Level is the minimum level to render. Debug records render as dim details.
	Level slog.Level
	// Color enables the semantic colors, see console.Palette.
	Color bool
}

// Handler is the slog handler for the text output.
type Handler struct {
	opts    Options
	palette console.Palette
	// attrs are the attributes added with WithAttrs, already nested in the
	// groups that were open at the time.
	attrs []slog.Attr
	// groups are the groups opened with WithGroup, outermost first. A record's
	// own attributes are nested in them when it is handled.
	groups []string
	out    *output
}

// output is what every derived handler shares: the writer and its lock, so
// that every record goes out as one write, and whether anything went out yet,
// so that the first step does not open with a blank line.
type output struct {
	mu      sync.Mutex
	w       io.Writer
	started bool
}

// NewHandler creates a handler writing to the writer.
func NewHandler(writer io.Writer, opts Options) *Handler {
	return &Handler{
		opts:    opts,
		palette: console.Palette{Enabled: opts.Color},
		out:     &output{w: writer},
	}
}

// Enabled implements slog.Handler.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.opts.Level
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), nest(h.groups, attrs)...)

	return &clone
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	clone := *h
	clone.groups = append(append([]string(nil), h.groups...), name)

	return &clone
}

// Handle implements slog.Handler.
func (h *Handler) Handle(_ context.Context, record slog.Record) error {
	own := make([]slog.Attr, 0, record.NumAttrs())

	record.Attrs(func(attr slog.Attr) bool {
		own = append(own, attr)

		return true
	})

	attrs := flatten("", append(append([]slog.Attr(nil), h.attrs...), nest(h.groups, own)...))
	depth, attrs := splitDepth(attrs)

	if record.Level < slog.LevelInfo && depth == 0 {
		depth = 1
	}

	indent := strings.Repeat(" ", depth*indentWidth)

	// A message with line breaks of its own, an error with a log tail in it for
	// one, keeps every line under the indent, so the shape survives it.
	line := indent + strings.ReplaceAll(h.format(record, attrs), "\n", "\n"+indent+"   ") + "\n"

	// One write per record, so the erase-before-write wrapper and the progress
	// bar see a whole line at a time.
	h.out.mu.Lock()
	defer h.out.mu.Unlock()

	// A step opens a paragraph: a blank line before it, unless it is the first
	// thing written. Details stay tight under their step.
	if depth == 0 && h.out.started {
		line = "\n" + line
	}

	h.out.started = true

	if _, err := io.WriteString(h.out.w, line); err != nil {
		return fmt.Errorf("failed to write log line: %w", err)
	}

	return nil
}

// format renders the message and its attributes as one line. A message that
// brings its own line break, as a library's can, does not get a second one.
func (h *Handler) format(record slog.Record, attrs []slog.Attr) string {
	message := strings.TrimRight(record.Message, "\r\n")

	switch {
	case record.Level >= slog.LevelError:
		message = h.palette.Bad(message)
	case record.Level >= slog.LevelWarn:
		message = h.palette.Warn(message)
	case record.Level < slog.LevelInfo:
		message = h.palette.Dim(message)
	}

	if len(attrs) == 0 {
		return message
	}

	parts := make([]string, 0, len(attrs))

	for _, attr := range attrs {
		parts = append(parts, attr.Key+"="+formatValue(attr.Value))
	}

	return message + " " + h.palette.Dim(strings.Join(parts, " "))
}

// nest wraps the attributes in the open groups, outermost first, the way the
// JSON handler nests them. With no open group they are returned as they are.
func nest(groups []string, attrs []slog.Attr) []slog.Attr {
	for _, group := range slices.Backward(groups) {
		attrs = []slog.Attr{{Key: group, Value: slog.GroupValue(attrs...)}}
	}

	return attrs
}

// flatten turns nested groups into dotted keys, drops empty attributes, and
// inlines a group without a name, as the slog handler contract asks.
func flatten(prefix string, attrs []slog.Attr) []slog.Attr {
	flat := make([]slog.Attr, 0, len(attrs))

	for _, attr := range attrs {
		if attr.Equal(slog.Attr{}) {
			continue
		}

		attr.Value = attr.Value.Resolve()

		if attr.Value.Kind() != slog.KindGroup {
			flat = append(flat, slog.Attr{Key: prefix + attr.Key, Value: attr.Value})

			continue
		}

		groupPrefix := prefix
		if attr.Key != "" {
			groupPrefix += attr.Key + "."
		}

		flat = append(flat, flatten(groupPrefix, attr.Value.Group())...)
	}

	return flat
}

// splitDepth takes the indent attributes out of the flattened attributes and
// returns the depth they add up to, clamped to what the shape allows. The
// attribute counts wherever it sits in the groups, so a detail logger derived
// from a grouped logger stays a detail, and a detail logger wrapped again goes
// deeper. An indent that is not a whole number is someone else's attribute and
// stays.
func splitDepth(attrs []slog.Attr) (int, []slog.Attr) {
	depth := 0
	rest := make([]slog.Attr, 0, len(attrs))

	for _, attr := range attrs {
		isIndent := attr.Key == IndentKey || strings.HasSuffix(attr.Key, "."+IndentKey)
		if isIndent && attr.Value.Kind() == slog.KindInt64 {
			depth += int(attr.Value.Int64())

			continue
		}

		rest = append(rest, attr)
	}

	return clampDepth(depth), rest
}

func clampDepth(depth int) int {
	return min(max(depth, 0), maxDepth)
}

// formatValue renders a value the way a reader expects to see it after a key:
// bare when it is one printable word, quoted when it is not. Quoting also keeps
// a control character in a value from acting on the terminal.
func formatValue(value slog.Value) string {
	text := value.String()

	needsQuotes := text == "" || strings.ContainsAny(text, "\"=") ||
		strings.IndexFunc(text, func(r rune) bool { return unicode.IsSpace(r) || !unicode.IsPrint(r) }) >= 0

	if needsQuotes {
		return strconv.Quote(text)
	}

	return text
}
