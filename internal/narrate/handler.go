// Package narrate indents the details of a step under it in the text log. A
// detail is an ordinary record that carries its depth as an attribute, so the
// JSON handler keeps it as a field, and the text handler, tint, gets the record
// with the depth turned into indentation.
package narrate

import (
	"context"
	"log/slog"
	"strings"
)

const (
	// IndentKey is the attribute a detail record carries: how deep it sits
	// under the step above it. A record without it is a step.
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

// Handler indents each record's message by its depth and drops the depth
// attribute before passing the record on.
type Handler struct {
	next  slog.Handler
	depth int
}

// NewHandler wraps the handler that draws the text log.
func NewHandler(next slog.Handler) *Handler {
	return &Handler{next: next}
}

// Enabled implements slog.Handler.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	depth, rest := splitDepth(attrs)

	clone := &Handler{next: h.next, depth: h.depth + depth}
	if len(rest) > 0 {
		clone.next = h.next.WithAttrs(rest)
	}

	return clone
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name), depth: h.depth}
}

// Handle implements slog.Handler.
func (h *Handler) Handle(ctx context.Context, record slog.Record) error {
	attrs := make([]slog.Attr, 0, record.NumAttrs())

	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)

		return true
	})

	depth, rest := splitDepth(attrs)
	depth = min(max(h.depth+depth, 0), maxDepth)

	// a library's message can bring its own line break, which would break the line
	message := strings.Repeat(" ", depth*indentWidth) + strings.TrimRight(record.Message, "\r\n")

	indented := slog.NewRecord(record.Time, record.Level, message, record.PC)
	indented.AddAttrs(rest...)

	return h.next.Handle(ctx, indented) //nolint:wrapcheck
}

// splitDepth takes the indent attributes out and returns the depth they add up
// to. An indent that is not a whole number is someone else's attribute and stays.
func splitDepth(attrs []slog.Attr) (int, []slog.Attr) {
	depth := 0
	rest := make([]slog.Attr, 0, len(attrs))

	for _, attr := range attrs {
		if attr.Key == IndentKey && attr.Value.Kind() == slog.KindInt64 {
			depth += int(attr.Value.Int64())

			continue
		}

		rest = append(rest, attr)
	}

	return depth, rest
}
