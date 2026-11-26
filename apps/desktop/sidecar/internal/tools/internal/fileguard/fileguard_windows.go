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

//go:build windows

package fileguard

import (
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// OpenFlags is a plain read: Windows has no FIFO in the filesystem.
const OpenFlags = os.O_RDONLY

// noFollow is nothing: Windows has no such flag, so a link swapped in between
// the tag read and the open is followed.
const noFollow = 0

// splitRoot is an absolute path as its volume and the rest from the root's
// separator on. A path opening with two separators — \\host\share, \\?\ and
// \\.\ alike — is refused first: a UNC path would reach out to another machine
// on open.
func splitRoot(path string) (vol, rest string, err error) {
	if len(path) >= 2 && os.IsPathSeparator(path[0]) && os.IsPathSeparator(path[1]) {
		return "", "", ErrNotLocal
	}
	vol = filepath.VolumeName(path)
	rest = path[len(vol):]
	if rest == "" || !os.IsPathSeparator(rest[0]) {
		return "", "", ErrNotAbs
	}
	return vol, rest, nil
}

// native is a plain path as Windows opens it: a Git Bash drive path read as
// native, and either separator as the platform's, so C:/Users/x and C:\Users\x
// key one stamp. A rooted path with no drive (\x, /tmp) is not absolute.
func native(path string) (string, error) {
	if drive, ok := tools.GitBashDrive(path); ok {
		path = drive
	}
	if !filepath.IsAbs(path) {
		return "", ErrNotAbs
	}
	// Plain, so Clean changes nothing but the separators.
	return filepath.Clean(path), nil
}

// plainForm is vol+rest cleaned, in the separator the model wrote it with, so
// the refusal hands back a path Abs takes: a Git Bash path stays one.
func plainForm(vol, rest string) string {
	clean := vol + pathpkg.Clean(filepath.ToSlash(rest))
	if strings.Contains(rest, `\`) {
		return filepath.FromSlash(clean)
	}
	return clean
}

// The reparse tags that are links: a symbolic link and a junction.
const (
	tagSymlink    = 0xA000000C // IO_REPARSE_TAG_SYMLINK
	tagMountPoint = 0xA0000003 // IO_REPARSE_TAG_MOUNT_POINT
)

// lstat is path's own info, and whether it is a link. Go reports a junction
// and a cloud file alike as ModeIrregular, so a reparse point is told by its
// tag: a link tag is a link, and any other (OneDrive, deduplication) is judged
// by what Stat says of the file behind it.
func lstat(path string) (os.FileInfo, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, false, err
	}
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
		return info, false, nil
	}
	tag, err := reparseTag(path)
	if err != nil {
		return nil, false, err
	}
	if tag == tagSymlink || tag == tagMountPoint {
		return info, true, nil
	}
	info, err = os.Stat(path)
	return info, false, err
}

// reparseTag reads path's reparse tag off its directory entry, opening nothing.
func reparseTag(path string) (uint32, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var data syscall.Win32finddata
	h, err := syscall.FindFirstFile(p, &data)
	if err != nil {
		return 0, err
	}
	_ = syscall.FindClose(h)
	if data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return 0, nil
	}
	return data.Reserved0, nil
}
