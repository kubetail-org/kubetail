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

package bash

import "strings"

// quote is s as one single-quoted word: a quote inside it closes the word, adds
// an escaped quote and reopens it. That is the whole escaping rule for POSIX
// shells and zsh.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// wrapper is what the shell runs for one command: the snapshot sourced, zsh
// set to read bash-style text as bash would, then the command through eval
// with stdin closed, so a prompt fails at once rather than hanging. The
// command is quoted into it whole, so what runs is what was approved.
func wrapper(kind, snapshot, command string) string {
	var b strings.Builder
	if snapshot != "" {
		b.WriteString("source ")
		b.WriteString(quote(snapshot))
		b.WriteString(" 2>/dev/null || true\n")
	}
	if kind == "zsh" {
		// # ~ ^ and a trailing (...) are glob syntax to zsh, not to bash; a glob
		// matching nothing aborts the line (kubectl's -o jsonpath={.items[*]});
		// and an unquoted $x is one word.
		b.WriteString("setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL NO_NOMATCH SH_WORD_SPLIT 2>/dev/null || true\n")
	}
	b.WriteString("eval ")
	b.WriteString(quote(command))
	b.WriteString(" < /dev/null")
	return b.String()
}
