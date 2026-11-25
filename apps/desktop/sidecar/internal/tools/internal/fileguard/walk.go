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

package fileguard

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

// What Walk refuses.
var (
	// ErrHidden is a path the sandbox keeps shut under the grant.
	ErrHidden = errors.New("files: a path the sandbox keeps shut")
	// ErrMoved is a granted folder, or a folder on its way, that is a link now.
	ErrMoved = errors.New("files: the granted folder has moved or become a link")
	// ErrLeaves is a link that leads out of the granted folder.
	ErrLeaves = errors.New("files: a link that leads out of the granted folder")
	// ErrTooManyLinks is a walk past maxLinks links.
	ErrTooManyLinks = errors.New("files: too many links")
)

// WalkRefusal is what a file tool tells the model of a walk's refusal, in one
// wording for every tool, and false for any other error.
func WalkRefusal(err error) (string, bool) {
	switch {
	case errors.Is(err, ErrHidden):
		return "The sandbox keeps this path closed, whatever folder the user granted.", true
	case errors.Is(err, ErrMoved):
		return "The granted folder has moved or become a link since it was granted. The user can grant it again.", true
	case errors.Is(err, ErrLeaves):
		return "A link on this path leads out of the granted folder.", true
	case errors.Is(err, ErrTooManyLinks):
		return "Too many links on the way to this path.", true
	}
	return "", false
}

// maxLinks is how many links one walk follows, as the kernel bounds a lookup.
const maxLinks = 40

// walkHook runs before each step of a walk opens name under dir: a test's
// seam, nil in production.
var walkHook func(dir, name string)

// Walk is path, inside folder, as a file tool reaches a granted folder: by
// handle, from /. Every folder from / to the grant is opened without following
// a link, so a grant that moved or became a link is ErrMoved; inside it a link
// is followed by reading it and walking again from the grant's handle, and one
// that leads out is ErrLeaves. Each handle is checked once it is open, so a
// path through a closed one is ErrHidden wherever a link pointed. The closed
// paths are resolved as the walk starts, so a link on the list meets its
// target of now. A missing part of path is left to the File: Create makes it
// on the handle of the deepest folder that exists. The caller closes the File.
func (f Fence) Walk(folder session.Folder, path string) (File, error) {
	h, ok := f.Hidden()
	if !ok {
		return File{}, ErrHidden
	}
	h = h.resolved()
	w := walk{grant: folder.Path, never: statAll(h.Never)}
	for _, c := range h.Closed {
		// A closed folder over the grant is the folder the grant opens.
		if !realpath.Same(c, folder.Path) && realpath.Under(c, folder.Path) {
			w.closed = append(w.closed, statAll([]string{c})...)
		}
	}
	grant, err := w.openGrant()
	if err != nil {
		return File{}, err
	}
	rel, ok := Under(folder.Path, resolvePaths([]string{path})[0])
	if !ok {
		grant.Close()
		return File{}, ErrLeaves
	}
	dir, name, err := w.descend(grant, rel)
	if err != nil {
		return File{}, err
	}
	return File{
		path: path, root: dir, name: name, hidden: slices.Concat(w.never, w.closed),
		// descend answers more than one part only from the first that is missing.
		ancestorMissing: strings.ContainsRune(name, filepath.Separator),
	}, nil
}

// walk is one Walk: the grant's path, and the closed paths' info, read once
// as it starts.
type walk struct {
	grant         string
	never, closed []os.FileInfo
}

// statAll is the info of each of paths that exists.
func statAll(paths []string) []os.FileInfo {
	var infos []os.FileInfo
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			infos = append(infos, info)
		}
	}
	return infos
}

// hides reports whether info is a closed path: a Never one, or below the
// grant's own handle a Closed one.
func (w walk) hides(info os.FileInfo, belowGrant bool) bool {
	same := func(h os.FileInfo) bool { return os.SameFile(h, info) }
	return slices.ContainsFunc(w.never, same) || belowGrant && slices.ContainsFunc(w.closed, same)
}

// openGrant opens the grant from /, each folder on the way without following
// a link, and checks each handle.
func (w walk) openGrant() (*os.Root, error) {
	dir, err := os.OpenRoot(string(filepath.Separator))
	if err != nil { // coverage:ignore the root directory always opens
		return nil, err
	}
	for _, name := range splitPath(strings.TrimPrefix(w.grant, string(filepath.Separator))) {
		next, link, err := w.openDir(dir, name, false)
		dir.Close()
		switch {
		case link:
			return nil, ErrMoved
		case err != nil:
			return nil, err
		}
		dir = next
	}
	return dir, nil
}

// openDir opens the folder name under dir without following a link: link is
// true, and nothing opened, when name is one. The handle opened must be the
// folder the Lstat saw, and not a closed path.
func (w walk) openDir(dir *os.Root, name string, belowGrant bool) (next *os.Root, link bool, err error) {
	if walkHook != nil {
		walkHook(dir.Name(), name)
	}
	info, err := dir.Lstat(name)
	switch {
	case err != nil:
		return nil, false, missing(err)
	case info.Mode()&os.ModeSymlink != 0:
		return nil, true, nil
	case !info.IsDir():
		return nil, false, ErrNotDir{Prefix: filepath.Join(dir.Name(), name)}
	}
	next, err = dir.OpenRoot(name)
	if err != nil {
		return nil, false, missing(err)
	}
	got, err := next.Stat(".")
	switch {
	case err != nil: // coverage:ignore a stat of a folder just opened fails only on an I/O error
	case w.hides(got, belowGrant):
		err = ErrHidden
	case !os.SameFile(info, got): // coverage:ignore only a folder swapped in between the Lstat and the open reaches it
		err = ErrChanged
	}
	if err != nil {
		next.Close()
		return nil, false, err
	}
	return next, false, nil
}

// descend walks rel from the grant's handle: the folder holding its last part,
// and that part, or from the first part that does not exist, the rest. A link
// on the way is read and walked again from the grant. It closes the grant
// unless it is the folder answered.
func (w walk) descend(grant *os.Root, rel string) (*os.Root, string, error) {
	dir, parts := grant, splitPath(rel)
	var at []string // the folders between the grant and dir, by name
	leave := func() {
		if dir != grant {
			dir.Close()
		}
	}
	fail := func(err error) (*os.Root, string, error) {
		leave()
		grant.Close()
		return nil, "", err
	}
	done := func(name string) (*os.Root, string, error) {
		if dir != grant {
			grant.Close()
		}
		return dir, name, nil
	}
	for links := 0; len(parts) > 0; {
		info, err := dir.Lstat(parts[0])
		switch {
		case absent(err):
			return done(filepath.Join(parts...))
		case err != nil:
			return fail(err)
		case info.Mode()&os.ModeSymlink != 0:
			if links++; links > maxLinks {
				return fail(ErrTooManyLinks)
			}
			target, err := dir.Readlink(parts[0])
			if err != nil { // coverage:ignore Lstat found a link here; only a swap in between fails reading it
				return fail(err)
			}
			next, ok := w.linkInside(at, target)
			if !ok {
				return fail(ErrLeaves)
			}
			leave()
			dir, at, parts = grant, nil, append(splitPath(next), parts[1:]...)
		case len(parts) == 1:
			if w.hides(info, true) {
				return fail(ErrHidden)
			}
			return done(parts[0])
		default:
			next, link, err := w.openDir(dir, parts[0], true)
			if link {
				// Swapped for a link since the Lstat: read it again.
				continue
			}
			if err != nil {
				return fail(err)
			}
			leave()
			dir, at, parts = next, append(at, parts[0]), parts[1:]
		}
	}
	// rel names the grant itself.
	return done(".")
}

// linkInside is where a link in the folder at, relative to the grant, leads,
// relative to the grant; false when it leads out of it. at holds no link, so
// a relative target is joined to it by name.
func (w walk) linkInside(at []string, target string) (string, bool) {
	if filepath.IsAbs(target) {
		return Under(w.grant, filepath.Clean(target))
	}
	rel := filepath.Join(append(slices.Clone(at), target)...)
	return rel, rel == "." || filepath.IsLocal(rel)
}

// splitPath is rel's parts, none for the grant itself.
func splitPath(rel string) []string {
	if rel == "." || rel == "" {
		return nil
	}
	return strings.Split(rel, string(filepath.Separator))
}
