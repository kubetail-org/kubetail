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

// Package unidiff reads a unified diff, as `git diff` writes it.
package unidiff

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// A hunk header, `@@ -a,b +c,d @@`: the old side's count, then the new side's
// first line and count.
var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Added returns the lines each file of the diff adds, in order, keyed by the
// file's path in the new tree.
func Added(r io.Reader) (map[string][]int, error) {
	added := map[string][]int{}
	var (
		file             string // the new side's path of the file being read
		line             int    // the new side's number of the hunk's next line
		oldLeft, newLeft int    // lines the hunk has yet to give of each side
	)
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		text := sc.Text()
		// A hunk's body is read by its header's counts, so an added line that
		// reads "++ x" is not taken for a file header.
		if oldLeft > 0 || newLeft > 0 {
			switch {
			case strings.HasPrefix(text, "+"):
				added[file] = append(added[file], line)
				line++
				newLeft--
			case strings.HasPrefix(text, "-"):
				oldLeft--
			case strings.HasPrefix(text, " "):
				line++
				oldLeft--
				newLeft--
			}
			continue
		}
		switch {
		case strings.HasPrefix(text, "+++ "):
			var err error
			if file, err = newPath(strings.TrimPrefix(text, "+++ ")); err != nil {
				return nil, err
			}
		case strings.HasPrefix(text, "@@ "):
			m := hunkHeader.FindStringSubmatch(text)
			if m == nil {
				return nil, fmt.Errorf("%q: not a hunk header", text)
			}
			line = count(m[2])
			oldLeft, newLeft = count(m[1]), count(m[3])
		}
	}
	return added, sc.Err()
}

// newPath reads the path of a `+++` line. Git ends a path holding a space
// with a tab, and C-quotes one holding a tab, quote, backslash, control or
// non-ASCII byte.
func newPath(s string) (string, error) {
	s = strings.TrimSuffix(s, "\t")
	if strings.HasPrefix(s, `"`) {
		// Git's C escapes are a subset of Go's.
		unquoted, err := strconv.Unquote(s)
		if err != nil {
			return "", fmt.Errorf("%q: %w", s, err)
		}
		s = unquoted
	}
	return strings.TrimPrefix(s, "b/"), nil
}

// count reads a number of a hunk header, which is 1 where the header leaves it
// out.
func count(digits string) int {
	if digits == "" {
		return 1
	}
	n, _ := strconv.Atoi(digits) // the header's pattern matched digits
	return n
}
