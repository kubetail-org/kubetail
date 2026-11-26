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
	"crypto/rand"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// PreviewLen is what a saved result shows of the text.
const PreviewLen = 2 << 10

// Saver writes text to a file of its own and answers the path the model is
// given, or ok false when it could not.
type Saver func(text string) (path string, ok bool)

// resultsName is the directory under a chat's where its saved results go.
const resultsName = "results"

// SaveTo is the Saver that writes a new file into the chat's results, named by
// the sidecar alone, so no provider's id is ever a filename. With no directory
// it always fails.
func SaveTo(dir ChatDir) Saver {
	return func(text string) (string, bool) {
		if dir == nil {
			return "", false
		}
		root, err := openIn(dir, resultsName, true)
		if err != nil {
			slog.Warn("could not open a chat's results to save a tool's result", "err", err)
			return "", false
		}
		defer root.Close()
		name := rand.Text() + ".txt"
		f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			slog.Warn("could not save a tool's result", "err", err)
			return "", false
		}
		_, err = f.WriteString(text)
		if err = errors.Join(err, f.Close()); err != nil { // coverage:ignore writing a file just created fails only on a full disk or an I/O error
			slog.Warn("could not save a tool's result", "err", err)
			_ = root.Remove(name)
			return "", false
		}
		return filepath.Join(dir.Path(), resultsName, name), true
	}
}

// Fit is header, text and trailer, whole when they fit InlineLimit. Past it,
// text cut at FileLimit goes to save and the result is header, a
// <persisted-output> block naming what was saved, then trailer; when the save
// fails, text is cut to fit with a note. discarded is what the producer dropped
// before text; what is the lower-case noun for text in the block and the note.
func Fit(save Saver, header, text, trailer string, discarded int, what string) string {
	if len(header)+len(text)+len(trailer) <= InlineLimit && discarded == 0 {
		return header + text + trailer
	}
	kept := text[:RuneBoundary(text, min(len(text), FileLimit))]
	if path, ok := save(kept); ok {
		return header + Persisted(path, kept, len(text)+discarded, what) + trailer
	}
	return header + Cut(text, discarded, InlineLimit-len(header)-len(trailer), "; the "+what+" could not be saved") + trailer
}

// Persisted is the block that stands for saved text: its size, the file, and
// the first PreviewLen bytes of it. size is what was produced; a kept shorter
// than it says how much the file lacks. Fit answers it; a tool that chooses its
// own fallback for a failed save builds it itself.
func Persisted(path, kept string, size int, what string) string {
	first := strings.ToUpper(what[:1]) + what[1:] + " too large (" + FormatSize(size) + "). Full " + what + " saved to: " + path
	if lost := size - len(kept); lost > 0 {
		first += "; " + strconv.Itoa(lost) + " bytes past the limit were not kept"
	}
	preview := kept[:RuneBoundary(kept, min(len(kept), PreviewLen))]
	return "<persisted-output>\n" + first + "\n\nPreview (first " + FormatSize(PreviewLen) + "):\n" + preview + "\n</persisted-output>"
}

// FormatSize is n bytes in KB below a megabyte and MB from one, 1024-based, to
// one decimal with a trailing .0 dropped.
func FormatSize(n int) string {
	value, unit := float64(n)/(1<<10), "KB"
	if n >= 1<<20 {
		value, unit = float64(n)/(1<<20), "MB"
	}
	return strings.TrimSuffix(strconv.FormatFloat(value, 'f', 1, 64), ".0") + unit
}

// Cut is text within budget bytes, ended with a note counting the bytes it
// lost and the bytes discarded before it, then tail, never splitting a rune.
// The note's own length comes off the budget, so the loop settles once the
// count's digits do, or at zero kept, where a budget the note does not fit gets
// the note cut to it.
func Cut(text string, discarded, budget int, tail string) string {
	budget = max(budget, 0)
	if len(text) <= budget && discarded == 0 {
		return text
	}
	keep := RuneBoundary(text, min(len(text), budget))
	for {
		note := "\n… [" + strconv.Itoa(len(text)-keep+discarded) + " bytes cut" + tail + "]"
		if keep+len(note) <= budget {
			return text[:keep] + note
		}
		if keep == 0 {
			return note[:RuneBoundary(note, budget)]
		}
		keep = RuneBoundary(text, max(budget-len(note), 0))
	}
}

// RuneBoundary is the largest index at or under limit that does not split a rune
// of s.
func RuneBoundary(s string, limit int) int {
	for limit > 0 && limit < len(s) && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return limit
}
