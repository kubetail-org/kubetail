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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// maxLinks is how many links Followed follows before it gives up on a path,
// so a cycle of links fails rather than hangs.
const maxLinks = 255

// Link is a link Followed met.
type Link struct {
	Name   string // the link, as the path or a link's target named it
	Parent string // the folder that holds it, as the kernel names it
}

// Parents is the folder holding each of links, once each.
func Parents(links []Link) []string {
	var parents []string
	for _, l := range links {
		if !slices.Contains(parents, l.Parent) {
			parents = append(parents, l.Parent)
		}
	}
	return parents
}

// Followed is p as the kernel names it, and each link met on the way. It walks
// p from the root one name at a time and, at a link, records it and goes on
// from its target, so a link inside a target is recorded too. A name that is
// missing ends the walk, and the rest is kept as given, as Canonical keeps
// it. A link whose target is missing is an error. Followed writes nothing.
func Followed(p string) (string, []Link, error) {
	at, rest := rootOf(p)
	var links []Link
	for len(rest) > 0 {
		name := rest[0]
		rest = rest[1:]
		switch name {
		case "", ".":
			continue
		case "..":
			at = filepath.Dir(at)
			continue
		}
		next := filepath.Join(at, name)
		info, err := os.Lstat(next)
		if isMissing(err) {
			at = filepath.Join(append([]string{next}, rest...)...)
			break
		}
		if err != nil {
			return "", nil, err
		}
		if !IsLink(info) {
			at = next
			continue
		}
		if len(links) == maxLinks {
			return "", nil, fmt.Errorf("%s: more than %d links", p, maxLinks)
		}
		target, err := os.Readlink(next)
		if err != nil && info.Mode()&fs.ModeSymlink == 0 { // coverage:ignore only a Windows reparse point that is no junction, such as a cloud file's, reaches this, and no test can make one
			// A reparse point Readlink cannot read is no link to follow: a
			// folder to walk through.
			at = next
			continue
		}
		if err != nil { // coverage:ignore Lstat just found the link; only its removal in between fails this
			return "", nil, fmt.Errorf("read the link %s: %w", next, err)
		}
		if _, err := os.Stat(next); errors.Is(err, fs.ErrNotExist) {
			return "", nil, fmt.Errorf("%s is a link to %s, which does not exist", next, target)
		}
		links = append(links, Link{Name: next, Parent: kernelPathOr(at)})
		// The target's names go first, unjoined: a .. after the link is the
		// parent of where the target leads.
		var targetNames []string
		switch {
		case filepath.IsAbs(target):
			at, targetNames = rootOf(target)
		// coverage:ignore-start os.Readlink answers a drive-rooted Windows target with its volume, so only a link it leaves as written reaches this
		case strings.HasPrefix(target, string(filepath.Separator)):
			// Windows' \foo: the root of the link's own volume.
			at, targetNames = rootOf(filepath.VolumeName(at) + target)
		// coverage:ignore-stop
		default:
			targetNames = names(target)
		}
		rest = append(targetNames, rest...)
	}
	got, err := canonical(at)
	return got, links, err
}

// rootOf is p's volume root and the names below it.
func rootOf(p string) (root string, rest []string) {
	vol := filepath.VolumeName(p)
	return vol + string(filepath.Separator), names(p[len(vol):])
}

// names is each name of p, in order.
func names(p string) []string {
	return strings.Split(filepath.ToSlash(p), "/")
}

// isMissing reports an error that ends a walk: a name that is not there, one
// this user may not reach, or a path through a file (ENOTDIR), which holds
// nothing.
func isMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.ENOTDIR)
}

// kernelPathOr is dir as the kernel names it, or as given when it cannot.
func kernelPathOr(dir string) string {
	k, err := KernelPath(dir)
	if err != nil { // coverage:ignore the walk just passed through dir; only its removal in between fails this
		return dir
	}
	return k
}
