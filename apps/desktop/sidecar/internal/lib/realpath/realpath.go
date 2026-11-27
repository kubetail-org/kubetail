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

// Package realpath tells which file a path names: it follows a path's links,
// names it as the kernel does, and compares two paths as files.
package realpath

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Within reports whether p is dir or lies under it, by their text alone. Text
// tells only for paths known to name no folder two ways, such as the canonical
// paths of one sandbox policy, and for a fixed path such as /proc; any other
// two paths are compared by Under.
func Within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// WithinAny reports whether p is Within any of dirs.
func WithinAny(p string, dirs []string) bool {
	return slices.ContainsFunc(dirs, func(d string) bool { return Within(p, d) })
}

// Resolved is p with its links followed. A path that does not exist is
// resolved through its deepest folder that does.
func Resolved(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	parent, name := split(p)
	if name == "" { // coverage:ignore EvalSymlinks of the root fails only when / cannot be read
		return filepath.Clean(p)
	}
	return filepath.Join(Resolved(parent), name)
}

// ResolvedAll is each of paths Resolved, so a caller comparing many paths
// against them resolves them once.
func ResolvedAll(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = Resolved(p)
	}
	return out
}

// split is p's parent and last name, as written. filepath.Dir cleans its
// answer, and cleaning drops a .. with the name before it, where the kernel
// follows a link there first and takes the parent of its target.
func split(p string) (parent, name string) {
	return filepath.Split(strings.TrimRight(p, string(filepath.Separator)))
}

// Canonical is each of paths as the kernel names it (KernelPath): its links
// followed, and on macOS each name spelled as the volume stores it. The error
// is the first path the kernel could not name, which is kept as given.
//
// Two paths of two calls are not compared by text: Linux has no stored
// spelling to read, so a folder that folds names keeps the spelling it was
// given. Under and Same compare them.
func Canonical(paths []string) ([]string, error) {
	out := make([]string, len(paths))
	var first error
	for i, p := range paths {
		var err error
		out[i], err = canonical(p)
		if first == nil {
			first = err
		}
	}
	return out, first
}

// canonical is p as the kernel names it. A path that is not there, or that
// this user may not reach, which no run of theirs can either, is named through
// its deepest folder that is, the rest kept as given.
func canonical(p string) (string, error) {
	at, err := KernelPath(p)
	switch {
	case err == nil:
		return at, nil
	case !isMissing(err):
		return filepath.Clean(p), fmt.Errorf("the path of %s on disk: %w", p, err)
	}
	parent, name := split(p)
	if name == "" { // coverage:ignore the root is always there
		return filepath.Clean(p), nil
	}
	// On an error dir is parent as given, so the join is p.
	dir, err := canonical(parent)
	return filepath.Join(dir, name), err
}

// Under reports whether p is dir or lies under it, as files: links are
// followed, and a folder on p that is the file dir is holds p however either
// is spelled, so a folder that folds names is one folder. Names past the
// deepest part of dir that is there are compared as given. It reads the disk.
func Under(p, dir string) bool {
	return ResolveDirs([]string{dir}).Holds(p)
}

// UnderAny reports whether p is Under any of dirs.
func UnderAny(p string, dirs []string) bool {
	return ResolveDirs(dirs).Holds(p)
}

// Same reports whether a and b are one path, as files (Under). It reads the
// disk.
func Same(a, b string) bool {
	return Under(a, b) && Under(b, a)
}

// Dirs is folders read from the disk once, for a caller that compares many
// paths against the same folders: UnderAny reads them again on every call.
type Dirs []dir

// dir is one folder of Dirs: resolved, and its deepest part that is there.
type dir struct {
	path  string
	there os.FileInfo
	rest  string
}

// ResolveDirs is dirs read for Holds. It reads the disk.
func ResolveDirs(dirs []string) Dirs {
	out := make(Dirs, len(dirs))
	for i, d := range dirs {
		path := Resolved(d)
		there, rest := deepest(path)
		out[i] = dir{path: path, there: there, rest: rest}
	}
	return out
}

// Holds reports whether p is Under any of ds. It reads the disk.
func (ds Dirs) Holds(p string) bool {
	p = Resolved(p)
	if slices.ContainsFunc(ds, func(d dir) bool { return Within(p, d.path) }) {
		return true
	}
	// Walking up from p, the first folder that is a dir's deepest part
	// decides that dir alone; a dir no folder matches does not hold p.
	decided := make([]bool, len(ds))
	for at := p; ; at = filepath.Dir(at) {
		if info, err := os.Lstat(at); err == nil {
			for i, d := range ds {
				if decided[i] || !os.SameFile(info, d.there) {
					continue
				}
				decided[i] = true
				if below, _ := filepath.Rel(at, p); Within(below, d.rest) {
					return true
				}
			}
		}
		if at == filepath.Dir(at) {
			return false
		}
	}
}

// deepest is the deepest part of p, resolved, that is there, and the names of
// p past it, "." for none.
func deepest(p string) (there os.FileInfo, rest string) {
	for at := p; ; at = filepath.Dir(at) {
		if info, err := os.Lstat(at); err == nil {
			rest, _ = filepath.Rel(at, p)
			return info, rest
		}
		if at == filepath.Dir(at) { // coverage:ignore the root is always there
			return nil, p
		}
	}
}
