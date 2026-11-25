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
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// owner is nothing on Windows: a file's ACL, not an owner and group, says who
// may write it.
type owner struct{}

func self() owner { return owner{} }

// replaceable checks the read-only attribute alone. A denying ACL on the file
// or its directory answers at the rename.
func replaceable(_ string, info os.FileInfo, _ owner) error {
	if info.Mode().Perm()&0o200 == 0 {
		return ErrReadOnly
	}
	return nil
}

// replaceableIn is replaceable for name under root.
func replaceableIn(_ *os.Root, _ string, info os.FileInfo, o owner) error {
	return replaceable("", info, o)
}

// syncDirIn is nothing: Windows cannot open a directory to flush it.
func syncDirIn(*os.Root, string) {}

// dirWritable is true: the directory's ACL answers at the write.
func dirWritable(string) bool { return true }

// keepGroup is nothing: Windows has no group to keep.
func keepGroup(*os.File, os.FileInfo) error { return nil }

// syncDir is nothing: Windows cannot open a directory to flush it.
func syncDir(string) {}

// renameNoReplace moves from to to, failing on a file there.
var renameNoReplace = func(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(f, t, 0)
}

// renameUnsupported is false: MoveFileEx without replacing works on every
// Windows filesystem.
func renameUnsupported(error) bool { return false }

// linkUnsupported is a hard link the volume does not have (FAT, exFAT, some
// shares). Only a create under a root links on Windows.
func linkUnsupported(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_FUNCTION)
}

// dirWritableIn is true: the directory's ACL answers at the write.
func dirWritableIn(os.FileInfo, owner) bool { return true }
