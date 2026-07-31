package migrator

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/utkuozdemir/pv-migrate/internal/console"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
)

const (
	outcomeDeclined = "declined"
	outcomeFailed   = "failed"
)

// attemptOutcome is what one rung of the ladder produced. The reasons are logged
// as the ladder runs, but by the time it is exhausted they have scrolled past,
// so they are kept here and reported again together.
type attemptOutcome struct {
	strategy    string
	declined    bool
	err         error
	diagnostics string
}

func (o attemptOutcome) status() string {
	if o.declined {
		return outcomeDeclined
	}

	return outcomeFailed
}

// message is what to print for this outcome. A decline carries its reason as a
// typed error, but a strategy is free to return a bare ErrUnaccepted with no
// reason, and that has to render as its own text rather than as a blank row.
func (o attemptOutcome) message() string {
	var declined *strategy.DeclinedError
	if errors.As(o.err, &declined) && declined.Reason != "" {
		return declined.Reason
	}

	if o.err == nil {
		return ""
	}

	return o.err.Error()
}

// ladderExhaustedError reports that every requested strategy declined or failed.
// Its message stays one line, because the CLI renders it as a single log
// attribute, and the per-strategy errors are reachable through the standard
// unwrap tree instead.
type ladderExhaustedError struct {
	errs []error
}

func (e *ladderExhaustedError) Error() string {
	return "no strategy could complete the migration"
}

func (e *ladderExhaustedError) Unwrap() []error {
	return e.errs
}

func newLadderExhaustedError(outcomes []attemptOutcome) error {
	errs := make([]error, 0, len(outcomes))

	for _, outcome := range outcomes {
		if outcome.err == nil {
			continue
		}

		errs = append(errs, fmt.Errorf("%s: %w", outcome.strategy, outcome.err))
	}

	return &ladderExhaustedError{errs: errs}
}

// reportOutcomes explains the exhausted ladder. The summary is a plain-text block
// on the request's writer, since packing it into the returned error would hand a
// multi-line string to a log handler that renders it as one attribute. When the
// logger writes JSON to that same stream, the block would corrupt it, so the
// outcomes go out as log records instead.
func reportOutcomes(request *migration.Request, outcomes []attemptOutcome, logger *slog.Logger) {
	if request.StructuredLogs {
		for _, outcome := range outcomes {
			args := []any{"strategy", outcome.strategy, "outcome", outcome.status(), "error", outcome.message()}
			if outcome.diagnostics != "" {
				args = append(args, "diagnostics", outcome.diagnostics)
			}

			// A decline is not a failure by this project's own invariant, so it
			// must not trip consumers filtering on the error level.
			if outcome.declined {
				logger.Warn("🦊 Strategy declined the migration", args...)

				continue
			}

			logger.Error("❌ Strategy did not complete the migration", args...)
		}

		return
	}

	writeSummary(request.Writer, outcomes, console.Palette{Enabled: request.ColorOutput})
}

// writeSummary explains the exhausted ladder. Decline reasons are short and sit
// in their row; failure messages are printed whole on their own indented lines,
// since a long error chain inside a table cell defeats the table on any real
// terminal width. A single-strategy run gets a sentence, because a one-row
// table about "no strategy" reads as if several were tried.
func writeSummary(writer io.Writer, outcomes []attemptOutcome, palette console.Palette) {
	fmt.Fprintln(writer)

	if len(outcomes) == 1 {
		outcome := outcomes[0]

		fmt.Fprintln(writer,
			palette.Failure(fmt.Sprintf("Migration failed: the %s strategy %s.", outcome.strategy, outcome.status())))
		writeIndented(writer, outcome.message())
	} else {
		nameWidth := 0
		for _, outcome := range outcomes {
			nameWidth = max(nameWidth, len(outcome.strategy))
		}

		fmt.Fprintln(writer, palette.Failure("Migration failed: no strategy could complete the migration."))
		fmt.Fprintln(writer)

		for _, outcome := range outcomes {
			// The status word is padded before coloring: escape codes have
			// width for the formatter but not for the terminal.
			name := fmt.Sprintf("%-*s", nameWidth, outcome.strategy)

			if outcome.declined {
				fmt.Fprintf(
					writer,
					"  %s  %s  %s\n",
					palette.Bold(name),
					palette.Warn(outcomeDeclined),
					outcome.message(),
				)

				continue
			}

			fmt.Fprintf(writer, "  %s  %s\n", palette.Bold(name), palette.Bad(outcomeFailed))
			writeIndented(writer, outcome.message())
		}
	}

	fmt.Fprintln(writer)

	writeDiagnosticsBlocks(writer, outcomes, palette)
}

func writeIndented(writer io.Writer, message string) {
	for line := range strings.SplitSeq(message, "\n") {
		fmt.Fprintf(writer, "    %s\n", line)
	}
}

// writeDiagnosticsBlocks prints what the cluster reported for the attempts that
// got as far as creating something. Declines never reach the cluster.
func writeDiagnosticsBlocks(writer io.Writer, outcomes []attemptOutcome, palette console.Palette) {
	printed := false

	for _, outcome := range outcomes {
		if outcome.diagnostics == "" {
			continue
		}

		if !printed {
			fmt.Fprintln(writer, palette.Bold("What the cluster reported:"))
			fmt.Fprintln(writer)

			printed = true
		}

		fmt.Fprint(writer, outcome.diagnostics)
	}

	if printed {
		fmt.Fprintln(writer)
	}
}
