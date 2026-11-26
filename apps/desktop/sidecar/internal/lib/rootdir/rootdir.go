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

// Package rootdir opens and removes directories through an os.Root, so a link
// a command leaves in one is refused or removed, never followed.
package rootdir

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
)

// ErrNotADirectory is a directory Open opens that is something other than a
// directory, a link included.
var ErrNotADirectory = errors.New("rootdir: not a directory")

// MakeRoot makes dir owner-only, with any parent it lacks, and opens it as a
// root. The caller closes the root.
func MakeRoot(dir string) (*os.Root, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}

// Sweep removes each of entries, listed from root, that keep refuses, through
// RemoveAll. An entry that will not go is logged and left for the next sweep.
// The caller lists root before reading what keep decides by: an owner commits
// what names an entry before making it, so every entry listed is one that
// read accounts for.
func Sweep(root *os.Root, entries []fs.DirEntry, keep func(fs.DirEntry) bool) {
	for _, e := range entries {
		if keep(e) {
			continue
		}
		if err := RemoveAll(root, e.Name()); err != nil {
			slog.Warn("could not remove a swept entry; the next start sweeps it", "dir", root.Name(), "entry", e.Name(), "err", err)
		}
	}
}

// Open opens name under parent as a root, reached through parent alone.
// create makes it, 0700, when it is missing; without create a missing one is
// fs.ErrNotExist. Anything but a directory, a link included, is
// ErrNotADirectory. The caller closes the root.
func Open(parent *os.Root, name string, create bool) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if create && errors.Is(err, fs.ErrNotExist) {
		// A concurrent call may make it first.
		if err = parent.Mkdir(name, 0o700); err == nil || errors.Is(err, fs.ErrExist) {
			info, err = parent.Lstat(name)
		}
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrNotADirectory
	}
	return openSameDir(parent, name, info)
}

// openSameDir opens name under parent and refuses it unless it is the directory
// want describes. OpenRoot follows a link that stays inside parent, so a link
// swapped in after want was read would otherwise be opened; once open, the root
// holds the directory itself and a later swap changes nothing.
func openSameDir(parent *os.Root, name string, want fs.FileInfo) (*os.Root, error) {
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	got, err := root.Stat(".")
	if err == nil && !os.SameFile(want, got) {
		err = ErrNotADirectory
	}
	if err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

// RemoveAll removes name under root, a link rather than its target, and is nil
// for a name already gone. A command can leave a directory its owner cannot
// write, such as a Go module cache, which RemoveAll cannot empty, so a failed
// removal makes the tree's directories writable and tries once more.
func RemoveAll(root *os.Root, name string) error {
	err := root.RemoveAll(name)
	if err != nil {
		if err = makeRemovable(root, name); err == nil {
			err = root.RemoveAll(name)
		}
	}
	return err
}

// makeRemovable gives each directory under name, name included, u+rwx, each
// before it is read, and skips every link. An entry that is not a directory is
// left alone: fs.WalkDir follows its starting point, so a link there would be
// walked into what it leads to.
func makeRemovable(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil || !info.IsDir() {
		return err
	}
	return fs.WalkDir(root.FS(), name, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		return root.Chmod(path, 0o700)
	})
}
