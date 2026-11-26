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
	"context"
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// What Replaceable refuses.
var (
	// ErrOtherUser is a file the user does not own: the rename would hand it to them.
	ErrOtherUser = errors.New("files: owned by another user")
	// ErrOtherGroup is a file in a group the new file could not be given.
	ErrOtherGroup = errors.New("files: in a group the new file cannot be given")
	// ErrReadOnly is a file the user cannot write.
	ErrReadOnly = errors.New("files: not writable")
)

// What Replace and Create answer.
var (
	// ErrCancelled is a write whose context ended before it changed anything.
	ErrCancelled = errors.New("files: cancelled")
	// ErrExists is a file put at a new file's path since the check.
	ErrExists = errors.New("files: a file is there")
	// ErrReplace is a rename over the old file that failed.
	ErrReplace = errors.New("files: could not replace")
	// ErrWrite is any other failure.
	ErrWrite = errors.New("files: could not write")
)

// ErrDirReadOnly is a directory the user cannot write in: Prefix, the file's
// own directory, or the deepest part of a new file's path that exists.
type ErrDirReadOnly struct{ Prefix string }

func (e ErrDirReadOnly) Error() string { return "files: cannot write in " + e.Prefix }

// ErrNotDir is a file where a new file's path needs a directory.
type ErrNotDir struct{ Prefix string }

func (e ErrNotDir) Error() string { return "files: not a directory: " + e.Prefix }

// Replaceable reports whether the file at path, whose own info is info, can be
// replaced by a rename that keeps its owner and group. It opens nothing.
func Replaceable(path string, info os.FileInfo) error {
	return replaceable(path, info, self())
}

// Replace puts content at path by a rename over the file old describes, so a
// reader never sees half a file. The new file keeps old's permission bits and
// group. ctx is checked before the rename, the one change on disk.
func Replace(ctx context.Context, path string, content []byte, old os.FileInfo) error {
	tmp, err := writeTemp(path, content, func(f *os.File) error {
		if err := keepGroup(f, old); err != nil {
			return err
		}
		return f.Chmod(old.Mode().Perm())
	})
	if err != nil {
		return ErrWrite
	}
	// A no-op once the rename has moved it.
	defer os.Remove(tmp)
	if ctx.Err() != nil {
		return ErrCancelled
	}
	if err := os.Rename(tmp, path); err != nil {
		return ErrReplace
	}
	syncDir(filepath.Dir(path))
	return nil
}

// Create puts content at path, which the caller found missing, making each
// missing directory on the way. The file takes 0o666 and each directory 0o777,
// less umask, set explicitly since the process's own umask is owner-only. It
// never replaces a file put there since: that is ErrExists. ctx is checked
// before the first directory and before the file is put in place; on any
// failure the directories it made go too.
func Create(ctx context.Context, path string, content []byte, umask fs.FileMode) (err error) {
	if ctx.Err() != nil {
		return ErrCancelled
	}
	made, err := makeDirs(filepath.Dir(path), umask)
	defer func() {
		if err != nil {
			removeDirs(made)
		}
	}()
	if err != nil {
		return err
	}
	perm := 0o666 &^ umask
	tmp, err := writeTemp(path, content, func(f *os.File) error { return f.Chmod(perm) })
	if err != nil {
		return ErrWrite
	}
	// Runs before the directories go: a no-op once a rename has moved it.
	defer os.Remove(tmp)
	if ctx.Err() != nil {
		return ErrCancelled
	}
	if err := placeNew(tmp, path, content, perm); err != nil {
		return err
	}
	syncDir(filepath.Dir(path))
	return nil
}

// makeDirs makes each missing directory up to dir, outermost first, and
// answers those it made. A directory another process made first is used and
// not counted; anything else found there, a link included, is ErrWrite, so a
// link planted in the way is never followed.
func makeDirs(dir string, umask fs.FileMode) ([]string, error) {
	var missing []string
	for p := dir; ; p = filepath.Dir(p) {
		if _, err := os.Stat(p); !absent(err) || filepath.Dir(p) == p {
			break
		}
		missing = append(missing, p)
	}
	var made []string
	for _, p := range slices.Backward(missing) {
		err := os.Mkdir(p, 0o700)
		if errors.Is(err, fs.ErrExist) {
			if info, lerr := os.Lstat(p); lerr == nil && info.IsDir() { // coverage:ignore only another process making the directory between the Stat and the Mkdir reaches it
				continue
			}
		}
		if err != nil {
			return made, ErrWrite
		}
		made = append(made, p)
		// Linux gives a directory made under a setgid parent the bit, and the
		// tree's group reaches its files only through it. The Chmod still drops
		// it for a user outside the directory's group: chmod(2) does so
		// silently, and only the umask at the Mkdir could avoid it.
		info, err := os.Lstat(p)
		if err != nil { // coverage:ignore the Mkdir just made it; only a removal in between fails the Lstat
			return made, ErrWrite
		}
		if err := os.Chmod(p, 0o777&^umask|info.Mode()&fs.ModeSetgid); err != nil { // coverage:ignore the user just made the directory; only a removal in between fails the Chmod
			return made, ErrWrite
		}
	}
	return made, nil
}

// removeDirs removes the directories made, innermost first, while each is
// empty.
func removeDirs(made []string) {
	for _, p := range slices.Backward(made) {
		_ = os.Remove(p)
	}
}

// link is os.Link, which a test swaps.
var link = os.Link

// placeNew puts tmp at path only while nothing is there, by a rename that
// refuses to replace. A filesystem without one (NFS, some FUSE mounts) gets a
// link, which also fails on a file there; one without hard links either (FAT,
// exFAT) gets content written through an O_EXCL open, which is not atomic but
// still never replaces a file. tmp is left for the caller to remove.
func placeNew(tmp, path string, content []byte, perm fs.FileMode) error {
	err := renameNoReplace(tmp, path)
	if renameUnsupported(err) {
		err = link(tmp, path)
		if linkUnsupported(err) {
			err = writeExcl(path, content, perm)
		}
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrExist):
		return ErrExists
	}
	return ErrWrite
}

// writeExcl writes content to a file it makes at path, and removes what it
// wrote on a failure.
func writeExcl(path string, content []byte, perm fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|noFollow, 0o600)
	if err != nil {
		return err
	}
	return fill(f, content, func(f *os.File) error { return f.Chmod(perm) })
}

// maxTempBase is the most of a file's name its temporary file carries, so the
// temporary name stays under NAME_MAX with its dot and suffix.
const maxTempBase = 200

// writeTemp writes content to a new temporary file beside path, syncs it, and
// hands it to finish before closing it. It answers the temporary file's path,
// and on any failure removes it.
func writeTemp(path string, content []byte, finish func(*os.File) error) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+cutName(filepath.Base(path))+".kubetail-*")
	if err != nil {
		return "", err
	}
	if err := fill(f, content, finish); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// fill writes content to the new file f, syncs it, hands it to finish and
// closes it. On any failure it removes the file.
func fill(f *os.File, content []byte, finish func(*os.File) error) error {
	err := writeClose(f, content, finish)
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}

// writeClose writes content to f, syncs it, hands it to finish and closes it.
func writeClose(f *os.File, content []byte, finish func(*os.File) error) error {
	_, err := f.Write(content)
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = finish(f)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// cutName is name cut to maxTempBase bytes on a rune boundary.
func cutName(name string) string {
	if len(name) <= maxTempBase {
		return name
	}
	i := maxTempBase
	for !utf8.RuneStart(name[i]) {
		i--
	}
	return name[:i]
}

// Creatable reports whether a file can be made at path, which does not exist:
// the deepest part of it that exists must be a directory the user can write and
// search. It opens nothing.
func Creatable(path string) error {
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		info, err := os.Stat(p)
		switch {
		case err == nil && !info.IsDir():
			return ErrNotDir{Prefix: p}
		case err == nil && !dirWritable(p):
			return ErrDirReadOnly{Prefix: p}
		case err == nil:
			return nil
		case !absent(err) || filepath.Dir(p) == p:
			return err
		}
	}
}

// The write forms under a root make the checks and changes their path forms
// make, each through a call the root offers: os.Root exposes no descriptor, so
// there is no access(2), renameat2 or CreateTemp under one.

// ReplaceableIn is Replaceable for name under root.
func ReplaceableIn(root *os.Root, name string, info os.FileInfo) error {
	return replaceableIn(root, name, info, self())
}

// ReplaceIn is Replace for name under root.
func ReplaceIn(ctx context.Context, root *os.Root, name string, content []byte, old os.FileInfo) error {
	tmp, err := writeTempIn(root, name, content, func(f *os.File) error {
		if err := keepGroup(f, old); err != nil {
			return err
		}
		return f.Chmod(old.Mode().Perm())
	})
	if err != nil {
		return ErrWrite
	}
	// A no-op once the rename has moved it.
	defer root.Remove(tmp)
	if ctx.Err() != nil {
		return ErrCancelled
	}
	if err := root.Rename(tmp, name); err != nil {
		return ErrReplace
	}
	syncDirIn(root, filepath.Dir(name))
	return nil
}

// writeTempIn is writeTemp for name under root: the temporary file is made
// by an exclusive create under a name rand.Text makes unique, since the root
// has no CreateTemp.
func writeTempIn(root *os.Root, name string, content []byte, finish func(*os.File) error) (string, error) {
	tmp := filepath.Join(filepath.Dir(name), "."+cutName(filepath.Base(name))+".kubetail-"+rand.Text())
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if err := writeClose(f, content, finish); err != nil {
		_ = root.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// CreatableIn is Creatable for name under root: the deepest part of it that
// exists must be a directory the user can write and search. A link on the way
// is refused wherever it leads, since the file tools never write through one.
func CreatableIn(root *os.Root, name string) error {
	dir := "."
	for part := range strings.SplitSeq(filepath.Dir(name), string(filepath.Separator)) {
		if part == "." {
			break
		}
		next := filepath.Join(dir, part)
		info, err := root.Lstat(next)
		switch {
		case absent(err):
			return creatableDir(root, dir)
		case err != nil:
			return err
		case info.Mode()&os.ModeSymlink != 0:
			target, err := linkTargetIn(root, next)
			if err != nil { // coverage:ignore Lstat found a link here; only a swap in between fails reading it
				return err
			}
			return ErrLink{Target: target}
		case !info.IsDir():
			return ErrNotDir{Prefix: filepath.Join(root.Name(), next)}
		}
		dir = next
	}
	return creatableDir(root, dir)
}

// creatableDir refuses dir under root unless the user can make entries in it.
func creatableDir(root *os.Root, dir string) error {
	info, err := root.Stat(dir)
	if err != nil { // coverage:ignore dir is the root or a folder the Lstat just found; only a removal in between fails the Stat
		return err
	}
	if !dirWritableIn(info, self()) {
		return ErrDirReadOnly{Prefix: filepath.Join(root.Name(), dir)}
	}
	return nil
}

// CreateIn is Create for name under root, which the caller found missing. The
// new file is placed by a link from the temporary file, which fails on a file
// there as the no-replace rename does, then the temporary file is removed.
func CreateIn(ctx context.Context, root *os.Root, name string, content []byte, umask fs.FileMode) (err error) {
	if ctx.Err() != nil {
		return ErrCancelled
	}
	made, err := makeDirsIn(root, filepath.Dir(name), umask)
	defer func() {
		if err != nil {
			removeDirsIn(root, made)
		}
	}()
	if err != nil {
		return err
	}
	perm := 0o666 &^ umask
	tmp, err := writeTempIn(root, name, content, func(f *os.File) error { return f.Chmod(perm) })
	if err != nil {
		return ErrWrite
	}
	// Runs before the directories go.
	defer root.Remove(tmp)
	if ctx.Err() != nil {
		return ErrCancelled
	}
	if err := placeNewIn(root, tmp, name, content, perm); err != nil {
		return err
	}
	syncDirIn(root, filepath.Dir(name))
	return nil
}

// linkIn is os.Root.Link, which a test swaps.
var linkIn = (*os.Root).Link

// placeNewIn puts tmp at name only while nothing is there: by a link, or on a
// filesystem without hard links (FAT, exFAT, some shares) by an O_EXCL write,
// which is not atomic but still never replaces a file. tmp is left for the
// caller to remove.
func placeNewIn(root *os.Root, tmp, name string, content []byte, perm fs.FileMode) error {
	err := linkIn(root, tmp, name)
	if linkUnsupported(err) {
		err = writeExclIn(root, name, content, perm)
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrExist):
		return ErrExists
	}
	return ErrWrite
}

// writeExclIn is writeExcl for name under root.
func writeExclIn(root *os.Root, name string, content []byte, perm fs.FileMode) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := writeClose(f, content, func(f *os.File) error { return f.Chmod(perm) }); err != nil { // coverage:ignore writing, chmodding and closing a file just made fails only on a failing disk
		_ = root.Remove(name)
		return err
	}
	return nil
}

// makeDirsIn is makeDirs for dir under root: each missing directory, outermost
// first, one Mkdir at a time with an Lstat after it, so anything but a
// directory found there is ErrWrite.
func makeDirsIn(root *os.Root, dir string, umask fs.FileMode) ([]string, error) {
	var missing []string
	for p := dir; p != "."; p = filepath.Dir(p) {
		if _, err := root.Lstat(p); !absent(err) {
			break
		}
		missing = append(missing, p)
	}
	var made []string
	for _, p := range slices.Backward(missing) {
		err := root.Mkdir(p, 0o700)
		if errors.Is(err, fs.ErrExist) { // coverage:ignore the Lstat found the name missing; only another process making it in between reaches it
			if info, lerr := root.Lstat(p); lerr == nil && info.IsDir() {
				continue
			}
		}
		if err != nil {
			return made, ErrWrite
		}
		made = append(made, p)
		// The setgid bit, as makeDirs keeps it.
		info, err := root.Lstat(p)
		if err != nil || !info.IsDir() { // coverage:ignore the Mkdir just made it; only a swap in between fails the Lstat
			return made, ErrWrite
		}
		if err := root.Chmod(p, 0o777&^umask|info.Mode()&fs.ModeSetgid); err != nil { // coverage:ignore the user just made the directory; only a removal in between fails the Chmod
			return made, ErrWrite
		}
	}
	return made, nil
}

// removeDirsIn removes the directories made, innermost first, while each is
// empty.
func removeDirsIn(root *os.Root, made []string) {
	for _, p := range slices.Backward(made) {
		_ = root.Remove(p)
	}
}
