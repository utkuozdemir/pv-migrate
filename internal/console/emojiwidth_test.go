package console_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// variationSelector asks for the emoji form of a character that is otherwise
// drawn as text. Its presence in a marker is the whole bug: the terminal still
// measures the character it was applied to, which is one column, while the font
// paints the two column picture that was asked for.
const variationSelector = '️'

// TestEmojiPrefixesAreTwoColumnsWide walks every message in the repository that
// starts with an emoji and checks the terminal will give that emoji two columns.
//
// A terminal decides how wide a character is from its East Asian Width property
// rather than from what the font draws, so an emoji that is only an emoji by
// request spills over the space after it and lands on the first letter of the
// message.
//
// The fix is to use emoji that are wide in their own right, which is what this
// checks, because the alternative, padding the narrow ones with a second space,
// is invisible in the source and renders wrong in terminals that do measure the
// variation selector.
func TestEmojiPrefixesAreTwoColumnsWide(t *testing.T) {
	t.Parallel()

	// The package level width functions consult the process locale, where an
	// East Asian one calls ambiguous characters two columns wide. Ask for one
	// answer instead, since neither the recordings nor most users are there.
	widths := runewidth.NewCondition()
	widths.EastAsianWidth = false

	root := repoRoot(t)
	checked := 0

	for _, path := range trackedGoFiles(t, root) {
		checked += checkFile(t, widths, root, path)
	}

	assert.Positive(t, checked, "the walk found no emoji at all, so it is checking nothing")
}

func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	return root
}

// trackedGoFiles lists what the repository actually contains, rather than what
// happens to be sitting in the checkout. A scratch file that does not parse, or
// a worktree left underneath the repository by other work, would otherwise fail
// this test for reasons that have nothing to do with the branch under test.
func trackedGoFiles(t *testing.T, root string) []string {
	t.Helper()

	listed, err := exec.CommandContext(t.Context(), "git", "-C", root, "ls-files", "-z", "*.go").Output()
	require.NoError(t, err)

	paths := strings.FieldsFunc(string(listed), func(r rune) bool { return r == 0 })
	require.NotEmpty(t, paths)

	return paths
}

// checkFile reports how many emoji-prefixed literals it looked at.
func checkFile(t *testing.T, widths *runewidth.Condition, root, path string) int {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, path), nil, 0)
	require.NoError(t, err)

	found := 0

	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}

		text, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}

		prefix, ok := emojiPrefix(text)
		if !ok {
			return true
		}

		found++

		assert.Equal(t, 2, widths.StringWidth(prefix),
			"%s: %q leads with %q, which a terminal gives %d column(s), so the emoji paints over the text after it",
			path, text, prefix, widths.StringWidth(prefix))

		return true
	})

	return found
}

// emojiPrefix returns the leading grapheme cluster of a string when that cluster
// is an emoji. Taking the whole cluster matters: an emoji is often several code
// points, and only the cluster as a whole has a width.
func emojiPrefix(text string) (string, bool) {
	if text == "" {
		return "", false
	}

	graphemes := uniseg.NewGraphemes(text)
	if !graphemes.Next() {
		return "", false
	}

	cluster := graphemes.Str()
	if !isEmoji(cluster) {
		return "", false
	}

	return cluster, true
}

// isEmoji reports whether a grapheme cluster is one of the pictures this project
// uses to mark a log message.
//
// A cluster asking for the emoji form of a character counts however that
// character is encoded, which is what catches a marker whose base is an ordinary
// symbol, a digit or a letter. Beyond those it is a short list of blocks rather
// than the full emoji property, since the standard library carries no table for
// that and the point is to catch the marker at the front of a message, not to
// classify text. A picture drawn from a block not listed here, and not asking
// for the emoji form, goes unchecked, and so does one buried mid-message.
//
// The list does hold characters that are narrow on purpose and stay that way,
// the tick and the cross among them. Opening a message with one is reported,
// which is the answer this project wants: every message here opens with a
// picture, and one that arrives as flat text beside the rest is the thing being
// prevented.
func isEmoji(cluster string) bool {
	if strings.ContainsRune(cluster, variationSelector) {
		return true
	}

	return unicode.Is(emojiBlocks, []rune(cluster)[0])
}

var emojiBlocks = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x231A, Hi: 0x23FF, Stride: 1}, // watches, hourglasses, media controls
		{Lo: 0x2600, Hi: 0x27BF, Stride: 1}, // miscellaneous symbols and dingbats
		{Lo: 0x2B00, Hi: 0x2BFF, Stride: 1}, // arrows, stars and geometric shapes
	},
	R32: []unicode.Range32{
		{Lo: 0x1F000, Hi: 0x1FAFF, Stride: 1}, // the emoji planes proper
	},
}
