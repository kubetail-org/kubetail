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

package realpath

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Rel is how one path sits to a folder, as files.
type Rel int

const (
	Apart Rel = iota
	Inside
	SameFolder
)

// Relation is how p sits to dir, as files. Walking up from p, the first folder
// that is the file dir's deepest existing folder is decides it: the names of p
// past that folder are compared with dir's, exactly as given. Text alone never
// makes two folders one, and no letter case is folded, so siblings a
// case-sensitive folder holds as State and state are Apart. Links are
// followed. It reads the disk.
func Relation(p, dir string) Rel {
	return relation(p, dir, os.Stat)
}

// relation is Relation with stat injected, so a test can stand in for a
// case-sensitive folder where the filesystem has none.
func relation(p, dir string, stat func(string) (fs.FileInfo, error)) Rel {
	p, dir = filepath.Clean(p), filepath.Clean(dir)
	there, rest := deepestOf(dir, stat)
	for at := p; ; at = filepath.Dir(at) {
		if info, err := stat(at); err == nil && os.SameFile(info, there) {
			below := namesPast(at, p)
			switch {
			case below == rest:
				return SameFolder
			case rest == "" || strings.HasPrefix(below, rest+string(filepath.Separator)):
				return Inside
			}
			return Apart
		}
		if at == filepath.Dir(at) {
			return Apart
		}
	}
}

// deepestOf is the deepest folder on p that is there, and the names of p past
// it, "" for none.
func deepestOf(p string, stat func(string) (fs.FileInfo, error)) (there fs.FileInfo, rest string) {
	for at := p; ; at = filepath.Dir(at) {
		if info, err := stat(at); err == nil {
			return info, namesPast(at, p)
		}
		if at == filepath.Dir(at) { // coverage:ignore the root is always there
			return nil, p
		}
	}
}

// namesPast is the names of p below at, an ancestor of p, as written.
func namesPast(at, p string) string {
	return strings.TrimLeft(p[len(at):], string(filepath.Separator))
}
