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

package logline

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

// Colour and cursor sequences, in their ESC and C1 forms, leave the text alone; a byte
// that is not UTF-8 is the replacement character.
func TestStripControlRemovesSequences(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ in, want string }{
		"colour":       {"\x1b[31mred\x1b[0m", "red"},
		"cursor":       {"a\x1b[2Kb\x1b[1;1Hc", "abc"},
		"osc bel":      {"\x1b]0;title\x07text", "text"},
		"osc st":       {"\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\", "link"},
		"c1 csi":       {"a\u009b31mb", "ab"},
		"c1 osc":       {"a\u009d0;t\u009cb", "ab"},
		"other c1":     {"a\u0085b", "ab"},
		"del":          {"a\x7fb", "ab"},
		"c0":           {"a\x00b\rc\x08d", "abcd"},
		"tab kept":     {"\x1b[1ma\tb", "a\tb"},
		"lone esc":     {"a\x1bb", "ab"},
		"unterminated": {"a\x1b[31", "a"},
		"open osc":     {"a\x1b]0;title", "a"},
		"invalid utf8": {"a\xffb", "a�b"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, StripControl(tc.in))
		})
	}
}

// A line past 16 KiB is cut there on a character boundary and ends with an ellipsis.
func TestStripControlCutsALongLine(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", MaxMessage-1) + "é" + strings.Repeat("b", 4096)

	got := StripControl(long)

	assert.True(t, utf8.ValidString(got))
	assert.Equal(t, strings.Repeat("a", MaxMessage-1)+"…", got)
}

// Tabs count toward the limit like any other character.
func TestStripControlCutsALineOfTabs(t *testing.T) {
	t.Parallel()
	got := StripControl("\x1b[1m" + strings.Repeat("\t", MaxMessage+10))
	assert.Equal(t, strings.Repeat("\t", MaxMessage)+"…", got)
}

// A line within the limit is kept whole.
func TestStripControlKeepsAShortLine(t *testing.T) {
	t.Parallel()
	line := strings.Repeat("x", MaxMessage)
	assert.Equal(t, line, StripControl(line))
}

// Grep matches the text the user sees, not the escapes around it.
func TestGrepMatchesTheStrippedText(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`^error: x$`)
	assert.True(t, re.MatchString(StripControl("\x1b[31merror\x1b[0m: x")))
}

// KeepColor keeps SGR sequences in their ESC form and strips every other control as
// StripControl does.
func TestKeepColorKeepsOnlySGR(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ in, want string }{
		"colour":       {"\x1b[31mred\x1b[0m", "\x1b[31mred\x1b[0m"},
		"256 and rgb":  {"\x1b[38;5;208ma\x1b[38;2;1;2;3mb", "\x1b[38;5;208ma\x1b[38;2;1;2;3mb"},
		"colon form":   {"a\x1b[38:2::1:2:3mb", "ab"},
		"long param":   {"a\x1b[00001mb\x1b[1;00000mc", "abc"},
		"empty params": {"a\x1b[;1;mb", "a\x1b[;1;mb"},
		"bare reset":   {"a\x1b[mb", "a\x1b[mb"},
		"cursor":       {"a\x1b[2Kb\x1b[1;1Hc", "abc"},
		"private sgr":  {"a\x1b[?1mb", "ab"},
		"c1 csi":       {"a\u009b31mb", "ab"},
		"osc":          {"\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\", "link"},
		"lone esc":     {"a\x1bb", "ab"},
		"unterminated": {"a\x1b[31", "a"},
		"c0":           {"a\x00b\rc", "abc"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, KeepColor(tc.in))
		})
	}
}

// A sequence that would carry a line past the limit is not kept in part.
func TestKeepColorCutsBeforeASequence(t *testing.T) {
	t.Parallel()
	got := KeepColor(strings.Repeat("a", MaxMessage-2) + "\x1b[31mb")
	assert.Equal(t, strings.Repeat("a", MaxMessage-2)+"…", got)
}

// Grep Matches a kept line's text, not the colours in it.
func TestGrepMatchesAKeptLinesText(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`^error: x$`)
	assert.True(t, Matches(re, KeepColor("\x1b[31merror\x1b[0m: x")))
	assert.False(t, Matches(regexp.MustCompile(`31m`), KeepColor("\x1b[31merror")))
}
