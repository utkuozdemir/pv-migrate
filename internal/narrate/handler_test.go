package narrate_test

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/utkuozdemir/pv-migrate/internal/narrate"
)

func newLogger(color bool, level slog.Level) (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer

	return slog.New(narrate.NewHandler(&buf, narrate.Options{Level: level, Color: color})), &buf
}

// The shape: a blank line before every step but the first, details indented
// under their step without one, three spaces per level.
func TestShape(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger(false, slog.LevelInfo)
	detail := narrate.Detail(logger, 1)
	deeper := narrate.Detail(logger, 2)

	logger.Info("🚀 Migrating")
	detail.Info("📌 first detail")
	deeper.Info("🏃 under the first")
	detail.Info("🧭 second detail")
	logger.Info("🦊 mount does not apply")
	logger.Info("🚁 clusterip")
	detail.Info("📦 created release")
	logger.Info("✅ done")

	assert.Equal(t, `🚀 Migrating
   📌 first detail
      🏃 under the first
   🧭 second detail

🦊 mount does not apply

🚁 clusterip
   📦 created release

✅ done
`, buf.String())
}

func TestAttributesFollowTheMessage(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger(false, slog.LevelInfo)

	logger.With("migration_id", "abc12").Info("🚁 Trying strategy", "strategy", "clusterip",
		"timeout", 2*time.Minute, "error", errors.New("no route to host"), "empty", "")

	assert.Equal(t, "🚁 Trying strategy migration_id=abc12 strategy=clusterip timeout=2m0s "+
		"error=\"no route to host\" empty=\"\"\n", buf.String())
}

func TestIndentAttributeIsNotPrinted(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger(false, slog.LevelInfo)

	narrate.Detail(logger, 1).Info("📦 created release", "name", "x")

	assert.Equal(t, "   📦 created release name=x\n", buf.String(),
		"the depth shapes the line and is not an attribute of it")
}

func TestDepthIsClampedAndAddsUp(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger(false, slog.LevelInfo)

	narrate.Detail(logger, 7).Info("too deep")
	narrate.Detail(logger, -3).Info("a step, not a detail")
	logger.Info("raw", "indent", 9)
	narrate.Detail(narrate.Detail(logger, 1), 1).Info("handed down and wrapped again")

	assert.Equal(
		t,
		"      too deep\n\na step, not a detail\n      raw\n      handed down and wrapped again\n",
		buf.String(),
	)
}

func TestLineBreaksInAMessageKeepTheIndent(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger(false, slog.LevelInfo)

	narrate.Detail(logger, 1).Warn("🔶 failed: exit code 12\nlast lines of rsync output:\nconnection closed")

	assert.Equal(
		t,
		"   🔶 failed: exit code 12\n      last lines of rsync output:\n      connection closed\n",
		buf.String(),
		"an error that carries a log tail stays under its line instead of spilling out as steps",
	)
}

func TestGroupsFlattenToDottedKeys(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger(false, slog.LevelDebug)

	logger.WithGroup("pod").Info("🏃 running", "name", "sshd-1")
	logger.Debug("stats", slog.Group("progress", "transferred", 1024, "total", 4096), "source", "rsync")
	logger.Info("inline", slog.Group("", "x", 1))
	logger.WithGroup("outer").With("a", 1).WithGroup("inner").Info("nested", "b", 2)

	assert.Equal(t, "🏃 running pod.name=sshd-1\n"+
		"   stats progress.transferred=1024 progress.total=4096 source=rsync\n"+
		"\ninline x=1\n"+
		"\nnested outer.a=1 outer.inner.b=2\n", buf.String())
}

func TestDetailUnderAGroupStaysADetail(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger(false, slog.LevelInfo)

	logger.Info("step")
	narrate.Detail(logger.WithGroup("pod"), 1).Info("detail", "name", "sshd-1")

	assert.Equal(t, "step\n   detail pod.name=sshd-1\n", buf.String(),
		"the depth counts wherever the group put it, and is not printed")
}

func TestIndentThatIsNotANumberIsSomeoneElsesAttribute(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger(false, slog.LevelInfo)

	logger.Info("step", "indent", "sneaky")
	logger.Info("step", "indent", 1.5)

	assert.Equal(t, "step indent=sneaky\n\nstep indent=1.5\n", buf.String(),
		"a dependency logging through the default logger must not be able to break the process")
}

func TestEmptyAttributesAreDropped(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger(false, slog.LevelInfo)

	logger.LogAttrs(t.Context(), slog.LevelInfo, "step", slog.Attr{}, slog.String("kept", "yes"))

	assert.Equal(t, "step kept=yes\n", buf.String())
}

func TestDebugRecordsAreDimDetails(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger(false, slog.LevelDebug)

	logger.Info("step")
	logger.Debug("what the code saw")

	assert.Equal(t, "step\n   what the code saw\n", buf.String())

	filtered, out := newLogger(false, slog.LevelInfo)
	filtered.Debug("hidden")
	assert.Empty(t, out.String())
}

func TestColorsAreSemanticAndOptional(t *testing.T) {
	t.Parallel()

	plain, plainBuf := newLogger(false, slog.LevelInfo)
	plain.Warn("🔶 careful", "why", "reasons")
	assert.NotContains(t, plainBuf.String(), "\x1b[", "no escape sequences without color")

	colored, buf := newLogger(true, slog.LevelInfo)
	colored.Warn("🔶 careful", "why", "reasons")
	colored.Error("❌ failed")
	colored.Info("✅ fine")

	out := buf.String()
	assert.Contains(t, out, "\x1b[33m🔶 careful\x1b[0m", "a warning is yellow")
	assert.Contains(t, out, "\x1b[2mwhy=reasons\x1b[0m", "attributes are dim")
	assert.Contains(t, out, "\x1b[31m❌ failed\x1b[0m", "an error is red")
	assert.Contains(t, out, "\n✅ fine\n", "an info step is left as it is")
}

func TestMessagesAndValuesStayOnTheirLine(t *testing.T) {
	t.Parallel()

	logger, buf := newLogger(false, slog.LevelInfo)

	logger.Info("a library's message ends with a line break\n")
	logger.Info("step", "value", "carriage\rreturn", "escape", "\x1b[2J", "plain", "word")

	assert.Equal(t, "a library's message ends with a line break\n"+
		"\nstep value=\"carriage\\rreturn\" escape=\"\\x1b[2J\" plain=word\n", buf.String(),
		"no extra blank line from the message, and a control character in a value is shown, not sent")
}
