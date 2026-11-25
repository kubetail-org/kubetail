// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A cut never splits a rune, and the note counts what was lost.
func TestCutOutputKeepsRunesWhole(t *testing.T) {
	text := strings.Repeat("é", 20) // two bytes each
	got := Cut(text, 0, 5+len("\n… [36 bytes cut]"), "")
	assert.True(t, utf8.ValidString(got))
	assert.Equal(t, "éé\n… [36 bytes cut]", got)
}

// Whatever the budget, the text ends under it: the note's own length comes off
// what is kept, and a budget the note does not fit gets the note cut to it.
func TestCutOutputEndsUnderAnyBudget(t *testing.T) {
	text := strings.Repeat("x", 1000)
	for budget := -1; budget <= 1100; budget++ {
		got := Cut(text, 7, budget, "")
		assert.LessOrEqual(t, len(got), max(budget, 0), budget)
		assert.True(t, utf8.ValidString(got), budget)
	}
	assert.Equal(t, "short", Cut("short", 0, 100, ""), "what fits is left alone")
}

// A saved result's size is 1024-based, KB under a megabyte and MB from one, one
// decimal with a trailing .0 dropped.
func TestTheSizeReadsInKBOrMB(t *testing.T) {
	for n, want := range map[int]string{
		30_000:    "29.3KB",
		40_000:    "39.1KB",
		1 << 20:   "1MB",
		1_258_291: "1.2MB",
		8 << 20:   "8MB",
	} {
		assert.Equal(t, want, FormatSize(n), n)
	}
}

// failedSave is a Saver whose every save fails.
func failedSave(string) (string, bool) { return "", false }

// keep is a Saver that holds what it was given.
type keep struct{ saved string }

func (k *keep) save(text string) (string, bool) {
	k.saved = text
	return "/results/c1/X.txt", true
}

// Text that fits with its header comes back whole, and nothing is saved.
func TestFitKeepsWhatFits(t *testing.T) {
	var k keep
	text := strings.Repeat("x", InlineLimit-len("head\n"))
	assert.Equal(t, "head\n"+text, Fit(k.save, "head\n", text, "", 0, "page"))
	assert.Empty(t, k.saved)
}

// Past the limit the text is saved and the result is the header, then the block
// naming the file, in what's words, with the first 2 KB.
func TestFitSavesWithAPreviewNamedByWhat(t *testing.T) {
	var k keep
	text := strings.Repeat("line\n", 8000)

	got := Fit(k.save, "head\n", text, "", 0, "page")

	assert.Equal(t, text, k.saved)
	assert.Equal(t, "head\n<persisted-output>\nPage too large (39.1KB). Full page saved to: /results/c1/X.txt"+
		"\n\nPreview (first 2KB):\n"+text[:PreviewLen]+"\n</persisted-output>", got)
}

// Text past the file limit is saved cut at it, and the block says how much the
// file lacks.
func TestFitSavesTextPastTheFileLimitCutAtIt(t *testing.T) {
	var k keep
	got := Fit(k.save, "", strings.Repeat("x", FileLimit+10), "", 100, "output")
	assert.Len(t, k.saved, FileLimit)
	assert.Contains(t, got, "Output too large (8MB). Full output saved to: /results/c1/X.txt; 110 bytes past the limit were not kept\n")
}

// A failed save falls back to a cut, its note in what's words.
func TestAFailedSaveIsCut(t *testing.T) {
	got := Fit(failedSave, "head\n", strings.Repeat("x", InlineLimit+100), "", 0, "page")
	assert.LessOrEqual(t, len(got), InlineLimit)
	assert.True(t, strings.HasPrefix(got, "head\nxxx"))
	assert.Regexp(t, `\n… \[\d+ bytes cut; the page could not be saved\]$`, got)
}

// The trailer comes last, inline or after the saved-output block, and counts
// against the limit as the header does.
func TestFitCountsTheTrailer(t *testing.T) {
	const trailer = "\n(trailer)"
	var k keep
	text := strings.Repeat("x", InlineLimit-len("head\n")-len(trailer))
	assert.Equal(t, "head\n"+text+trailer, Fit(k.save, "head\n", text, trailer, 0, "page"))
	assert.Empty(t, k.saved)

	saved := Fit(k.save, "head\n", text+"x", trailer, 0, "page")
	assert.Equal(t, text+"x", k.saved)
	assert.True(t, strings.HasSuffix(saved, "</persisted-output>"+trailer))

	cut := Fit(failedSave, "head\n", text+"x", trailer, 0, "page")
	assert.LessOrEqual(t, len(cut), InlineLimit)
	assert.Regexp(t, `the page could not be saved\]\n\(trailer\)$`, cut)
}

// With no chat's directory every save fails, so Fit cuts.
func TestSaveToNothingFails(t *testing.T) {
	_, ok := SaveTo(nil)("text")
	assert.False(t, ok)
	assert.Regexp(t, `the page could not be saved\]$`, Fit(SaveTo(nil), "", strings.Repeat("x", InlineLimit+1), "", 0, "page"))
}

// A save is a new file in the results, under a name of the sidecar's own, made
// along with the directory.
func TestSaveToWritesAFileOfItsOwn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "c1")

	path, ok := SaveTo(testChatDir(dir))("text")

	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "results"), filepath.Dir(path))
	assert.Regexp(t, `^[A-Z2-7]{26}\.txt$`, filepath.Base(path), "rand.Text, never a provider's id")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "text", string(got))
}

// testChatDir is a chat's directory of the test's own.
type testChatDir string

func (d testChatDir) Path() string { return string(d) }

func (d testChatDir) Root(create bool) (*os.Root, error) {
	if create {
		if err := os.MkdirAll(string(d), 0o700); err != nil {
			return nil, err
		}
	}
	return os.OpenRoot(string(d))
}

// Persisted is the block Fit answers for saved text, for a tool that saves on its own.
func TestPersistedIsFitsBlock(t *testing.T) {
	text := strings.Repeat("x", InlineLimit+1)
	save := func(string) (string, bool) { return "/results/c1/X.txt", true }

	assert.Equal(t, Fit(save, "", text, "", 0, "result"), Persisted("/results/c1/X.txt", text, len(text), "result"))
}
