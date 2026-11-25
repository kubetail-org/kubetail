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
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
)

// maxMessage is the most of a line the sidecar sends, in bytes, before the ellipsis.
const MaxMessage = 16 << 10

// StripControl is a log line as plain text: CSI and OSC sequences, led by ESC or by
// their one-character C1 forms, every other C1 control, DEL and every C0 control but
// tab removed, invalid UTF-8 replaced, and the line cut at maxMessage on a character
// boundary with an ellipsis. The model, grep and a refusal's message read it.
func StripControl(s string) string {
	return Clean(s, false)
}

// KeepColor is StripControl with each SGR sequence (colour and text style) kept
// whole, in its ESC form. The file and the wire to the viewer hold it.
func KeepColor(s string) string {
	return Clean(s, true)
}

func Clean(s string, keepSGR bool) string {
	if plain(s) {
		return s
	}
	var b strings.Builder
	b.Grow(min(len(s), MaxMessage+len("…")))
	for i := 0; i < len(s); {
		start := i
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		// keep is what this step adds to the line: a character, a kept sequence, or
		// nothing for any other control.
		var keep string
		switch {
		case r == 0x1b && i < len(s) && s[i] == '[':
			i = SkipCSI(s, i+1)
			if keepSGR && isSGR(s[start:i]) {
				keep = s[start:i]
			}
		case r == 0x1b && i < len(s) && s[i] == ']':
			i = skipOSC(s, i+1)
		case r == 0x9b:
			i = SkipCSI(s, i)
		case r == 0x9d:
			i = skipOSC(s, i)
		case r < 0x20 && r != '\t', r == 0x7f, r >= 0x80 && r <= 0x9f:
		case r == utf8.RuneError && size == 1:
			keep = "\uFFFD"
		default:
			keep = s[start:i]
		}
		if b.Len()+len(keep) > MaxMessage {
			b.WriteString("…")
			break
		}
		b.WriteString(keep)
	}
	return b.String()
}

// plain is whether s is printable ASCII and tabs within maxMessage, which most
// lines are and which clean leaves as it is.
func plain(s string) bool {
	if len(s) > MaxMessage {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 0x20 && c != '\t') || c >= 0x7f {
			return false
		}
	}
	return true
}

// isSGR is whether seq, an ESC-led CSI sequence, selects graphic rendition in the
// form fancy-ansi both draws and strips: parameters of at most four digits each,
// separated by semicolons, final byte m. Its parser reads longer parameters, but its
// stripAnsi leaves them in the text, so a kept one would reach copy as text.
func isSGR(seq string) bool {
	if !strings.HasSuffix(seq, "m") {
		return false
	}
	for p := range strings.SplitSeq(seq[len("\x1b["):len(seq)-1], ";") {
		if len(p) > 4 || strings.Trim(p, "0123456789") != "" {
			return false
		}
	}
	return true
}

// skipCSI is the index past a control sequence whose parameters start at i: its
// final byte is the first in 0x40–0x7E, and an unterminated one runs to the end.
func SkipCSI(s string, i int) int {
	for ; i < len(s); i++ {
		if s[i] >= 0x40 && s[i] <= 0x7e {
			return i + 1
		}
	}
	return i
}

// skipOSC is the index past an operating system command whose text starts at i: it
// ends at BEL, at ESC \, or at the C1 string terminator, and an unterminated one runs
// to the end.
func skipOSC(s string, i int) int {
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == 0x07, r == 0x9c:
			return i + size
		case r == 0x1b && i+1 < len(s) && s[i+1] == '\\':
			return i + 2
		}
		i += size
	}
	return i
}

// MaxGrep is the longest grep a scope takes, in bytes. The wire takes any scope the
// webview sends, so the service checks it, and the LogsView tool's parse reads it.
const MaxGrep = 1_000

// CompileGrep is a scope's grep, nil for none, refusing one past MaxGrep or one
// that does not compile.
func CompileGrep(grep string) (*regexp.Regexp, error) {
	switch {
	case grep == "":
		return nil, nil
	case len(grep) > MaxGrep:
		return nil, refusal.New(refusal.CodeBadGrep, fmt.Sprintf("a grep is at most %d bytes", MaxGrep))
	}
	re, err := regexp.Compile(grep)
	if err != nil {
		return nil, refusal.New(refusal.CodeBadGrep, "the grep is not a regular expression")
	}
	return re, nil
}

// Matches is whether message passes grep, nil passing every line. The file keeps a
// line's colours; grep reads it without them.
func Matches(grep *regexp.Regexp, message string) bool {
	if grep == nil {
		return true
	}
	// A stored line's only control is a kept SGR sequence.
	if strings.Contains(message, "\x1b") {
		message = StripControl(message)
	}
	return grep.MatchString(message)
}
