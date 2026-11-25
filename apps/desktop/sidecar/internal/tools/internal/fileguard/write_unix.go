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

//go:build !windows

package fileguard

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"golang.org/x/sys/unix"
)

// owner is who the sidecar runs as, as a replace needs it.
type owner struct {
	uid    int
	groups []int
	// bsdGroups is a system where every new file takes its directory's group,
	// whatever the directory's mode.
	bsdGroups bool
}

// self is the sidecar's own owner.
func self() owner {
	groups, _ := os.Getgroups()
	return owner{uid: os.Getuid(), groups: append(groups, os.Getgid()), bsdGroups: bsdGroups}
}

func replaceable(path string, info os.FileInfo, o owner) error {
	st := info.Sys().(*syscall.Stat_t)
	if int(st.Uid) != o.uid {
		return ErrOtherUser
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !slices.Contains(o.groups, int(st.Gid)) && !inherits(dir, st.Gid, o) {
		return ErrOtherGroup
	}
	if unix.Access(path, unix.W_OK) != nil {
		return ErrReadOnly
	}
	if !dirWritable(filepath.Dir(path)) {
		return ErrDirReadOnly{Prefix: filepath.Dir(path)}
	}
	return nil
}

// replaceableIn is replaceable for name under root. access(2) takes a path, so
// writability is read off the owner's mode bits: the owner check has found the
// file the user's, and the directory must be the user's too.
func replaceableIn(root *os.Root, name string, info os.FileInfo, o owner) error {
	st := info.Sys().(*syscall.Stat_t)
	if int(st.Uid) != o.uid {
		return ErrOtherUser
	}
	dirName := filepath.Dir(name)
	dir, err := root.Stat(dirName)
	if err != nil {
		return err
	}
	if !slices.Contains(o.groups, int(st.Gid)) && !inherits(dir, st.Gid, o) {
		return ErrOtherGroup
	}
	if info.Mode().Perm()&0o200 == 0 {
		return ErrReadOnly
	}
	if !dirWritableIn(dir, o) {
		return ErrDirReadOnly{Prefix: filepath.Join(root.Name(), dirName)}
	}
	return nil
}

// dirWritableIn reports that the user owns dir and can make and rename entries
// in it.
func dirWritableIn(dir os.FileInfo, o owner) bool {
	return int(dir.Sys().(*syscall.Stat_t).Uid) == o.uid && dir.Mode().Perm()&0o300 == 0o300
}

// syncDirIn is syncDir for dir under root.
func syncDirIn(root *os.Root, dir string) { syncOpened(root.Open(dir)) }

// keepGroup gives f the group of the file old describes, when a new file in the
// directory did not take it already.
func keepGroup(f *os.File, old os.FileInfo) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	gid := old.Sys().(*syscall.Stat_t).Gid
	if info.Sys().(*syscall.Stat_t).Gid == gid {
		return nil
	}
	return f.Chown(-1, int(gid))
}

// syncDir makes a rename in dir survive a crash. A failure is ignored: the
// file is in place.
func syncDir(dir string) { syncOpened(os.Open(dir)) }

// syncOpened syncs and closes a directory opened for syncDir or syncDirIn.
func syncOpened(d *os.File, err error) {
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

// renameUnsupported is a no-replace rename the filesystem does not have.
func renameUnsupported(err error) bool {
	return errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) ||
		errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS)
}

// linkUnsupported is a hard link the filesystem does not have.
func linkUnsupported(err error) bool {
	return errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP)
}

// dirWritable reports that the user can make and rename entries in dir.
// access(2) opens nothing, so no watcher sees a write that never happened.
func dirWritable(dir string) bool {
	return unix.Access(dir, unix.W_OK|unix.X_OK) == nil
}

// inherits reports that a new file in dir takes gid without a Chown.
func inherits(dir os.FileInfo, gid uint32, o owner) bool {
	return dir.Sys().(*syscall.Stat_t).Gid == gid && (o.bsdGroups || dir.Mode()&os.ModeSetgid != 0)
}
