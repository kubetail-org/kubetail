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

package permissions

import (
	"regexp"
	"strings"
)

// Match is whether value matches pattern: * matches any run of characters, /
// and : included; ? matches one character; \ escapes the next; nothing else
// is special. A path's pattern goes through MatchPath instead.
func Match(pattern, value string) bool {
	var re strings.Builder
	re.WriteString(`(?s)^`)
	escaped := false
	for _, r := range pattern {
		switch {
		case escaped:
			re.WriteString(regexp.QuoteMeta(string(r)))
			escaped = false
		case r == '\\':
			escaped = true
		case r == '*':
			re.WriteString(`.*`)
		case r == '?':
			re.WriteString(`.`)
		default:
			re.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	if escaped { // a trailing \ is itself
		re.WriteString(`\\`)
	}
	re.WriteString(`$`)
	return regexp.MustCompile(re.String()).MatchString(value)
}

// Literal is the pattern that matches s alone.
func Literal(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '*' || r == '?' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// LiteralValue is the one value pattern matches, false for a pattern Literal
// would not have written.
func LiteralValue(pattern string) (string, bool) {
	var b strings.Builder
	escaped := false
	for _, r := range pattern {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
			continue
		case r == '*' || r == '?':
			return "", false
		}
		b.WriteRune(r)
	}
	return b.String(), !escaped && Literal(b.String()) == pattern
}

// MatchPath is whether value, a path of /-separated segments, matches
// pattern. A pattern that is * alone matches any value. Any other matches
// when it has as many segments as value and each segment Matches its own, so
// * never crosses /. A last segment of ** matches no segment or any number.
func MatchPath(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	segs, vals := strings.Split(pattern, "/"), strings.Split(value, "/")
	if last := len(segs) - 1; segs[last] == "**" {
		segs = segs[:last]
		if len(segs) == 0 {
			return true
		}
		if len(vals) < len(segs) {
			return false
		}
		vals = vals[:len(segs)]
	}
	if len(segs) != len(vals) {
		return false
	}
	for i, s := range segs {
		if !Match(s, vals[i]) {
			return false
		}
	}
	return true
}

// MatchPathBelow is whether pattern names paths below value: it has more
// segments than value, and its leading ones match value.
func MatchPathBelow(pattern, value string) bool {
	segs := strings.Split(pattern, "/")
	n := len(strings.Split(value, "/"))
	return len(segs) > n && MatchPath(strings.Join(segs[:n], "/"), value)
}

// MatchPathAndBelow is whether pattern matches value and every path below it:
// "*" alone, or a last ** whose leading segments match value.
func MatchPathAndBelow(pattern, value string) bool {
	return MatchPath(pattern, value) && (pattern == "*" || pattern == "**" || strings.HasSuffix(pattern, "/**"))
}
