package narrate_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/lmittmann/tint"
	"github.com/stretchr/testify/assert"

	"github.com/utkuozdemir/pv-migrate/internal/narrate"
)

func newLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer

	return slog.New(narrate.NewHandler(tint.NewTextHandler(&buf, &tint.Options{
		NoColor: true,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{}
			}

			return attr
		},
	}))), &buf
}

func TestDetailsAreIndentedUnderTheirStep(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger()
	detail := narrate.Detail(logger, 1)

	logger.Info("🚀 Migrating", "source", "ns/a")
	detail.Info("📌 Source claim", "pvc", "ns/a")
	narrate.Detail(detail, 1).Warn("🔶 Pod is not starting yet")
	narrate.Detail(detail, 5).Info("🔗 deeper than two")
	logger.Info("✅ Migration succeeded")

	assert.Equal(t, `INF 🚀 Migrating source=ns/a
INF    📌 Source claim pvc=ns/a
WRN       🔶 Pod is not starting yet
INF       🔗 deeper than two
INF ✅ Migration succeeded
`, buf.String())
}

func TestIndentStaysOutOfTheTextInsideAGroup(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger()

	narrate.Detail(logger.WithGroup("helm"), 1).Info("📦 Created release", "release", "r")

	assert.Equal(t, "INF    📦 Created release helm.release=r\n", buf.String())
}

func TestSomeoneElsesIndentStays(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger()

	logger.Info("📄 formatted", "indent", "tabs")

	assert.Equal(t, "INF 📄 formatted indent=tabs\n", buf.String())
}

func TestTrailingLineBreakOfAMessageIsDropped(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger()

	logger.Info("a library's message\r\n", "k", "v")

	assert.Equal(t, "INF a library's message k=v\n", buf.String())
}
