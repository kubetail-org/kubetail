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

// Package fileguard is the checks every file tool makes on a path, so the tools
// cannot disagree on them.
package fileguard

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// ErrNotAbs is a path that is not absolute.
var ErrNotAbs = errors.New("files: path is not absolute")

// ErrNotPlain is a path with an empty, "." or ".." component. Plain is the
// path it spells.
type ErrNotPlain struct{ Plain string }

func (e ErrNotPlain) Error() string { return "files: path is not plain; plain: " + e.Plain }

// ErrNotOneLine is a path longer than MaxPath bytes or holding a control
// character: the approval request draws the path whole, on one line.
var ErrNotOneLine = errors.New("files: path is not one line of at most 4096 bytes")

// MaxPath is the longest path Abs takes, in bytes: a PATH_MAX.
const MaxPath = 4096

// ErrNotLocal is a UNC or device path, which only Windows has.
var ErrNotLocal = errors.New("files: path is not on this machine")

// ErrUnresolved is a path whose place on disk cannot be told.
var ErrUnresolved = errors.New("files: path cannot be resolved")

// What Open and ReadAll refuse.
var (
	ErrMissing    = errors.New("files: no such file")
	ErrDirectory  = errors.New("files: a directory")
	ErrNotRegular = errors.New("files: not a regular file")
	ErrTooLarge   = errors.New("files: larger than the limit")
)

// ErrLink is a symbolic link in the last component. Target is where it leads,
// absolute and plain.
type ErrLink struct{ Target string }

func (e ErrLink) Error() string { return "files: a symbolic link to " + e.Target }

// Abs is path as a file tool opens it: a Git Bash drive path read as native.
// It refuses a path that is not one line (ErrNotOneLine), one that is not
// absolute ("~" included), one that is not plain (ErrNotPlain carries the
// plain form), and on Windows a UNC or device path. Plainness is checked on the path as the model wrote it, before the
// drive conversion: a plain path is one cleaning leaves as it is.
func Abs(path string) (string, error) {
	if len(path) > MaxPath || strings.ContainsFunc(path, unicode.IsControl) {
		return "", ErrNotOneLine
	}
	vol, rest, err := splitRoot(path)
	if err != nil {
		return "", err
	}
	slashed := filepath.ToSlash(rest)
	if pathpkg.Clean(slashed) != slashed {
		return "", ErrNotPlain{Plain: plainForm(vol, rest)}
	}
	return native(path)
}

// Fence is Kubetail's directories, which no file tool opens but a chat's
// workspace, and what the sandbox keeps shut under a folder grant.
type Fence struct {
	dirs []string // each absolute and clean
	// hidden answers the denied-always list with Kubetail's directories, and
	// the closed folders, each resolved; nil for a fence that knows of none.
	hidden func() (never, closed []string)
}

// ErrNoFence is a fence around no directory, which would hold nothing.
var ErrNoFence = errors.New("files: a fence needs a directory")

// NewFence is the fence around dirs, each made absolute as a chat's
// directory's path is. None is ErrNoFence.
func NewFence(dirs ...string) (Fence, error) {
	if len(dirs) == 0 {
		return Fence{}, ErrNoFence
	}
	f := Fence{dirs: make([]string, 0, len(dirs))}
	for _, dir := range dirs {
		abs, err := filepath.Abs(dir)
		if err != nil { // coverage:ignore filepath.Abs fails only when the working directory cannot be read
			return Fence{}, err
		}
		f.dirs = append(f.dirs, abs)
	}
	return f, nil
}

// Named reports whether path is under any of the fence's directories by name.
// It touches nothing on disk.
func (f Fence) Named(path string) bool {
	for _, dir := range f.dirs {
		if _, ok := Under(dir, path); ok {
			return true
		}
	}
	return false
}

// Under is path relative to dir, when it is under dir by name.
func Under(dir, path string) (string, bool) {
	rel, err := filepath.Rel(dir, path)
	return rel, err == nil && filepath.IsLocal(rel)
}

// Holds reports whether path is under the fence on disk. It resolves the
// longest prefix of path that exists, links followed, and walks it and each
// of its ancestors, comparing each with the fence's directories by
// os.SameFile, so a spelling in another case, a short name, or a link on the
// way is caught.
// ErrUnresolved is an ancestor that cannot be resolved for any reason but
// absence: the caller refuses, since it cannot tell.
func (f Fence) Holds(path string) (bool, error) {
	fenced := f.stat()
	resolved, err := longestExisting(path)
	if err != nil {
		return false, err
	}
	for dir := resolved; ; dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err != nil { // coverage:ignore longestExisting resolved this path just before; only a removal in between fails the Stat
			return false, ErrUnresolved
		}
		for _, d := range fenced {
			if os.SameFile(info, d) {
				return true, nil
			}
		}
		if filepath.Dir(dir) == dir {
			return false, nil
		}
	}
}

// stat reads each of the fence's directories, and makes one that is missing
// 0700, as the app made it. The OS or the user may clear a directory while
// Kubetail runs: one read earlier would no longer match the directory made in
// its place, and one missing would match nothing, so a path spelled another
// way would pass while a write made the directory under it. A directory that
// can be neither read nor made is left out: a write, as this user, could not
// make anything under it either.
func (f Fence) stat() []os.FileInfo {
	infos := make([]os.FileInfo, 0, len(f.dirs))
	for _, dir := range f.dirs {
		info, err := os.Stat(dir)
		if absent(err) {
			if err = os.MkdirAll(dir, 0o700); err == nil {
				info, err = os.Stat(dir)
			}
		}
		if err == nil {
			infos = append(infos, info)
		}
	}
	return infos
}

// longestExisting is the longest prefix of path that exists, its links
// resolved: a file not yet made is judged by the directory it would land in.
func longestExisting(path string) (string, error) {
	for p := path; ; p = filepath.Dir(p) {
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			return resolved, nil
		}
		if !absent(err) || filepath.Dir(p) == p {
			return "", ErrUnresolved
		}
	}
}

// Open opens path for reading when it is a regular file of at most
// tools.FileLimit bytes. The type is judged by an Lstat before the open, since
// opening some devices has side effects, and again by the open file's own Stat
// after, which catches a swap in between. A link in the last component is
// never followed.
func Open(path string) (*os.File, error) {
	info, err := Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := Check(info); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, OpenFlags|noFollow, 0)
	if err != nil {
		return nil, missing(err)
	}
	info, err = f.Stat()
	if err == nil {
		err = Check(info)
	}
	if err != nil { // coverage:ignore the Lstat passed these checks; only a file swapped in before the open fails them
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// Lstat is path's own info when it is a regular file, of any size: ErrMissing
// when nothing is there, ErrLink for a link in the last component, ErrDirectory
// and ErrNotRegular by type. It opens nothing.
func Lstat(path string) (os.FileInfo, error) {
	info, link, err := lstat(path)
	switch {
	case err != nil:
		return nil, missing(err)
	case link:
		target, err := linkTarget(path)
		if err != nil { // coverage:ignore lstat found a link here; only a swap in between fails reading it
			return nil, err
		}
		return nil, ErrLink{Target: target}
	case info.IsDir():
		return nil, ErrDirectory
	case !info.Mode().IsRegular():
		return nil, ErrNotRegular
	}
	return info, nil
}

// Check refuses what a file tool does not read, by the file's type and size.
func Check(info os.FileInfo) error {
	switch {
	case info.IsDir():
		return ErrDirectory
	case !info.Mode().IsRegular():
		return ErrNotRegular
	case info.Size() > tools.FileLimit:
		return ErrTooLarge
	}
	return nil
}

// linkTarget is where the link at path leads: a relative target joined to the
// link's directory with that directory's links resolved, since a lexical join
// would name another file.
func linkTarget(path string) (string, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target), nil
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil { // coverage:ignore the link's own lstat resolved its directory; only a change in between fails it
		return "", err
	}
	return filepath.Join(dir, target), nil
}

// ReadAll is the whole of f, through a LimitReader, since a writer can keep
// appending after the Stat.
func ReadAll(f *os.File) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(f, tools.FileLimit+1))
	if err != nil {
		return nil, err
	}
	if len(b) > tools.FileLimit {
		return nil, ErrTooLarge
	}
	return b, nil
}

// binaryWindow is how much of a file Binary looks at.
const binaryWindow = 8 << 10

// Binary reports a NUL byte in the first 8 KiB.
func Binary(b []byte) bool {
	return bytes.IndexByte(b[:min(len(b), binaryWindow)], 0) >= 0
}

// absent reports an error that says a path does not exist: missing, or a
// component that is a file.
func absent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// missing is err as Open answers it: ErrMissing when the path does not exist.
func missing(err error) error {
	if absent(err) {
		return ErrMissing
	}
	return err
}

// The root forms take a directory opened as an os.Root and a name under it, and
// make the checks their path forms make. The root refuses any name that leads
// out of it, a link on the way included; a link at the name is refused as the
// path forms refuse one.

// ErrChanged is a file that was swapped for another between its Lstat and its
// open.
var ErrChanged = errors.New("files: changed while it was opened")

// LstatIn is name's own info when it is a regular file, as Lstat
// answers for a path.
func LstatIn(root *os.Root, name string) (os.FileInfo, error) {
	info, err := root.Lstat(name)
	switch {
	case err != nil:
		return nil, missing(err)
	case info.Mode()&os.ModeSymlink != 0:
		target, err := linkTargetIn(root, name)
		if err != nil { // coverage:ignore Lstat found a link here; only a swap in between fails reading it
			return nil, err
		}
		return nil, ErrLink{Target: target}
	case info.IsDir():
		return nil, ErrDirectory
	case !info.Mode().IsRegular():
		return nil, ErrNotRegular
	}
	return info, nil
}

// OpenIn opens name for reading as Open opens a path. The root follows a link
// that stays inside it, so the open file must be the one the Lstat saw.
func OpenIn(root *os.Root, name string) (*os.File, error) {
	info, err := LstatIn(root, name)
	if err != nil {
		return nil, err
	}
	if err := Check(info); err != nil {
		return nil, err
	}
	f, err := root.OpenFile(name, OpenFlags, 0)
	if err != nil {
		return nil, missing(err)
	}
	got, err := f.Stat()
	switch {
	case err == nil && !os.SameFile(info, got): // coverage:ignore only a file swapped in between the Lstat and the open reaches it
		err = ErrChanged
	case err == nil:
		err = Check(got)
	}
	if err != nil { // coverage:ignore the Lstat passed these checks; only a file swapped in before the open fails them
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// linkTargetIn is where the link at name leads, absolute and plain: a relative
// target joined to the link's directory under the root.
func linkTargetIn(root *os.Root, name string) (string, error) {
	target, err := root.Readlink(name)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target), nil
	}
	return filepath.Join(root.Name(), filepath.Dir(name), target), nil
}
