package console_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/utkuozdemir/pv-migrate/internal/console"
)

func TestPaletteDisabledIsPlainText(t *testing.T) {
	t.Parallel()

	var palette console.Palette

	assert.Equal(t, "x", palette.Failure("x"))
	assert.Equal(t, "x", palette.Bad("x"))
	assert.Equal(t, "x", palette.Warn("x"))
	assert.Equal(t, "x", palette.Good("x"))
	assert.Equal(t, "x", palette.Dim("x"))
	assert.Equal(t, "x", palette.Bold("x"))
}

func TestPaletteEnabledWrapsAndResets(t *testing.T) {
	t.Parallel()

	palette := console.Palette{Enabled: true}

	assert.Equal(t, "\x1b[31mx\x1b[0m", palette.Bad("x"))
	assert.Equal(t, "\x1b[1;31mx\x1b[0m", palette.Failure("x"))
	assert.Empty(t, palette.Bad(""), "coloring nothing must stay nothing")
}

func TestForTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	assert.True(t, console.ForTerminal(true, false))
	assert.False(t, console.ForTerminal(true, true), "colors inside a structured stream corrupt it")
	assert.False(t, console.ForTerminal(false, false))
}

func TestForTerminalHonorsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	assert.False(t, console.ForTerminal(true, false))
}
